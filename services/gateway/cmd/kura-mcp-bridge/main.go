// Command kura-mcp-bridge is the gateway's in-Pod process: it serves the
// suite MCP surface and the two health endpoints on loopback, and Caddy
// forwards to it.
//
// It calls the leaf services directly rather than back through Caddy. Looping
// back would make the health fan-out route into this process and would force
// shutdown to be ordered so Caddy never drained mid-call.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wyvernzora/kura/services/gateway/internal/client"
	"github.com/wyvernzora/kura/services/gateway/internal/config"
	"github.com/wyvernzora/kura/services/gateway/internal/health"
	gwmcp "github.com/wyvernzora/kura/services/gateway/internal/mcp"
	"github.com/wyvernzora/kura/services/gateway/internal/metrics"
)

// version is overridden at link time via -ldflags, as both leaves do. It is
// deliberately not a config key: a ConfigMap value would lag the image tag and
// make /api/v1/health and X-Kura-Version lie.
var version = "dev"

const drainTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", config.DefaultPath, "path to gateway.toml")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}

	cfg, err := config.Load(*configPath, os.Getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// Each subsystem stamps its own "component" on a shared base —
	// deriving them from a base that already carries component=bridge
	// would emit duplicate JSON keys (slog does not dedupe).
	baseLogger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	logger := baseLogger.With("component", "bridge")

	opts := client.Options{
		RequestTimeout:   cfg.RequestTimeout,
		MaxResponseBytes: cfg.MaxResponseBytes,
	}
	library := client.New(cfg.LibraryUpstream, "/api/library/v1", opts)
	releases := client.New(cfg.ReleasesUpstream, "/api/releases/v1", opts)

	healthz := health.New(version, []health.Leaf{
		{Name: "libraryManager", URL: library.HealthURL()},
		{Name: "releaseIndexer", URL: releases.HealthURL()},
	}, cfg.ComponentTimeout)
	healthz.Logger = baseLogger.With("component", "health")

	metricsSrv := metrics.New(version)
	bridge := gwmcp.New(version, library, releases, baseLogger.With("component", "mcp"))
	bridge.Metrics = metricsSrv
	mcpHandler := bridge.Handler(cfg.SessionTimeout)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz.Local)
	mux.HandleFunc("GET /api/v1/health", healthz.System)
	mux.Handle(gwmcp.Endpoint, mcpHandler)
	mux.Handle(gwmcp.Endpoint+"/", mcpHandler)

	srv := &http.Server{Addr: cfg.Address, Handler: mux}

	// Bind synchronously so a failed bind exits non-zero promptly instead
	// of leaving a process up that serves nothing.
	ln, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return fmt.Errorf("bind %s: %w", cfg.Address, err)
	}
	logger.Info("bridge listening",
		"addr", ln.Addr().String(),
		"version", version,
		"library_upstream", cfg.LibraryUpstream,
		"releases_upstream", cfg.ReleasesUpstream,
	)

	// /metrics on its own pod-reachable listener: Caddy owns :9090 in this
	// Pod, and the loopback MCP listener must stay unreachable from the
	// network, so exposition gets a third socket.
	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", metricsSrv.Handler())
	metricsHTTP := &http.Server{Handler: metricsMux}
	metricsLn, err := net.Listen("tcp", cfg.MetricsAddress)
	if err != nil {
		return fmt.Errorf("bind metrics %s: %w", cfg.MetricsAddress, err)
	}
	logger.Info("bridge metrics listening", "addr", metricsLn.Addr().String())
	go func() { _ = metricsHTTP.Serve(metricsLn) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
		defer cancel()
		_ = metricsHTTP.Shutdown(shutCtx)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		logger.Info("bridge shutting down")
		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), drainTimeout)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			logger.Warn("graceful shutdown timed out", "err", err)
			_ = srv.Close()
		}
		return nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	}
}

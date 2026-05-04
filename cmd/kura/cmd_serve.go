package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/progress"
	mcpserver "github.com/wyvernzora/kura/internal/server/mcp"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/workflow"
)

// defaultShutdownTimeout caps how long cmd_serve waits for the jobs
// registry to drain after the transport ctx is cancelled.
const defaultShutdownTimeout = 10 * time.Second

type serveCmd struct {
	MCPStdio bool   `name:"mcp-stdio" help:"Run MCP transport over stdio (Claude Desktop, mcp inspector --stdio)."`
	MCPHTTP  string `name:"mcp-http" placeholder:"ADDR" help:"Run MCP transport over streamable HTTP at the given address (e.g. ':8080' or '127.0.0.1:8080')."`
}

func (cmd *serveCmd) Run(rt *runContext) error {
	if !cmd.MCPStdio && cmd.MCPHTTP == "" {
		return errors.New("kura serve requires at least one transport flag (--mcp-stdio or --mcp-http=ADDR)")
	}

	logger := newServerLogger(rt.Stderr, rt.Getenv)

	// Suppress the CLI spinner globally for the server lifetime BEFORE
	// any registry / coordinator captures rt.Context. Otherwise the
	// jobs registry's parentCtx retains the spinner reporter installed
	// by run.go, and every async job's progress events tee back into
	// the spinner — emitting ANSI control sequences into the log
	// stream. Lifecycle visibility comes from structured logs instead.
	rt.Context = progress.With(rt.Context, func(context.Context, progress.Event) {})

	deps, registry, watch, err := buildServeDeps(rt, logger)
	if err != nil {
		logger.Error("server bootstrap failed", "err", err)
		return err
	}

	// Manual signal wiring (vs signal.NotifyContext) so the signal name
	// can be logged at the moment it arrives — before transports start
	// draining. Goroutine cancels ctx on first signal; subsequent
	// signals are ignored (kernel default would force-kill).
	ctx, cancel := context.WithCancel(rt.Context)
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case sig := <-sigCh:
			logger.Info("shutdown signal received, draining", "signal", sig.String())
			cancel()
		case <-ctx.Done():
		}
	}()

	deps.Index.Watch(ctx, watch)

	server := mcpserver.NewServer(mcpserver.Deps{Workflow: deps, Logger: logger})

	logger.Info("kura serve starting",
		"version", serveVersion,
		"libRoot", deps.LibRoot,
		"transports", serverTransports(cmd),
	)

	g, gctx := errgroup.WithContext(ctx)
	if cmd.MCPStdio {
		g.Go(func() error { return mcpserver.ServeStdio(gctx, server) })
	}
	if cmd.MCPHTTP != "" {
		addr := cmd.MCPHTTP
		g.Go(func() error { return mcpserver.ServeHTTP(gctx, addr, server) })
	}

	runErr := g.Wait()

	grace := envDuration(rt.Getenv, "KURA_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if stuck := registry.Shutdown(grace); stuck > 0 {
		logger.Warn("jobs did not shut down within grace period", "stuck", stuck, "grace", grace)
	} else {
		logger.Info("jobs registry drained", "grace", grace)
	}

	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		logger.Error("kura serve exited with error", "err", runErr)
		return runErr
	}
	logger.Info("kura serve stopped cleanly")
	return nil
}

// serveVersion is the human-readable version embedded in the boot
// log. Mirrors the constant inside the MCP server package.
const serveVersion = "0.1.0"

// serverTransports returns the transport names enabled by the CLI
// flags, for inclusion in the boot log.
func serverTransports(cmd *serveCmd) []string {
	var out []string
	if cmd.MCPStdio {
		out = append(out, "mcp-stdio")
	}
	if cmd.MCPHTTP != "" {
		out = append(out, "mcp-http="+cmd.MCPHTTP)
	}
	return out
}

// buildServeDeps wraps buildDeps and swaps the in-process serializer
// (CLI no-op → MCP per-series mutex) plus a long-lived jobs registry
// configured from KURA_JOB_* envs. Returns the WatchConfig the caller
// should pass to deps.Index.Watch under the signal-cancellable ctx.
// The CLI registry from buildDeps is discarded; it never received a
// Submit so no goroutines leak.
//
// The supplied logger is bound to the jobs registry so job lifecycle
// events ("job submitted", "job terminal", "reaper evicted") flow
// into the same structured log stream as the boot/transport events.
func buildServeDeps(rt *runContext, logger *slog.Logger) (workflow.Deps, *jobs.Registry, indexfile.WatchConfig, error) {
	// Async index path: any cold-start rebuild proceeds in the
	// background. kura_list returns server_not_ready until the rebuild
	// completes; transports come up immediately.
	deps, err := buildDepsAsyncIndex(rt)
	if err != nil {
		return workflow.Deps{}, nil, indexfile.WatchConfig{}, err
	}
	attempts := envInt(rt.Getenv, "KURA_CONFLICT_RETRIES", 1) + 1
	deps.Coordinator = coord.NewMCPCoordinator(coord.MaxAttempts(attempts))

	registry := jobs.NewRegistry(rt.Context, jobs.Config{
		JobTimeout:     envDuration(rt.Getenv, "KURA_JOB_TIMEOUT", 0),
		Retention:      envDuration(rt.Getenv, "KURA_JOB_RETENTION", 30*time.Minute),
		ReaperInterval: envDuration(rt.Getenv, "KURA_JOB_REAPER_INTERVAL", 5*time.Minute),
	}, logger)
	deps.Jobs = registry
	deps.Logger = logger

	watch := indexfile.WatchConfig{
		ProbeInterval:   envDuration(rt.Getenv, "KURA_INDEX_PROBE_INTERVAL", 2*time.Second),
		RefreshInterval: envDuration(rt.Getenv, "KURA_INDEX_REFRESH_INTERVAL", 5*time.Minute),
		RebuildInterval: envDuration(rt.Getenv, "KURA_INDEX_REBUILD_INTERVAL", time.Hour),
		Builder:         indexfile.BuildRow,
		Logger:          logger,
	}
	return deps, registry, watch, nil
}

func envDuration(getenv func(string) string, key string, fallback time.Duration) time.Duration {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return fallback
	}
	return d
}

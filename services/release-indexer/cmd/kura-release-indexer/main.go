// Command kura-release-indexer serves the release index and runs configured
// source crawlers. Matching remains external.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	dbmigrations "github.com/wyvernzora/kura/services/release-indexer/db/migrations"
	"github.com/wyvernzora/kura/services/release-indexer/internal/config"
	"github.com/wyvernzora/kura/services/release-indexer/internal/crawlrunner"
	"github.com/wyvernzora/kura/services/release-indexer/internal/health"
	"github.com/wyvernzora/kura/services/release-indexer/internal/ingest"
	"github.com/wyvernzora/kura/services/release-indexer/internal/mcp"
	"github.com/wyvernzora/kura/services/release-indexer/internal/metrics"
	"github.com/wyvernzora/kura/services/release-indexer/internal/rest"
	"github.com/wyvernzora/kura/services/release-indexer/internal/store/postgres"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/rawpost"
	"github.com/wyvernzora/kura/services/release-indexer/sources/dmhy"
	"github.com/wyvernzora/kura/services/release-indexer/sources/nyaa"

	// time/tzdata bakes the IANA zoneinfo database into the binary so
	// timezone-dependent parsing works in distroless/scratch images that
	// lack /usr/share/zoneinfo.
	_ "time/tzdata"
)

// version and commit are overridable at link time via -ldflags.
var (
	version = "0.1.0"
	commit  = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "takuhai:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, showVersion, err := loadConfig()
	if err != nil {
		return err
	}
	if showVersion {
		fmt.Println(version)
		return nil
	}

	level, err := parseLogLevel(cfg.LogLevel)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Bring the schema to head BEFORE serving (the embedded goose migrations are
	// idempotent — a database already at head is a no-op). A migration failure aborts
	// startup with a non-zero exit; we never serve against an unmigrated database.
	logger.Info("running migrations")
	if err := runMigrations(ctx, cfg.DatabaseURL); err != nil {
		logger.Error("migrations failed", "err", err)
		return fmt.Errorf("run migrations: %w", err)
	}
	logger.Info("migrations complete")

	// Construct the Postgres store.
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database connection failed", "err", err)
		return fmt.Errorf("connect database: %w", err)
	}
	st := postgres.NewStoreWithConfig(pool, nil, postgres.StoreConfig{
		QueueMaxAttempts: cfg.QueueMaxAttempts,
	})
	defer st.Close() //nolint:errcheck // best-effort cleanup at process exit.

	// The single mountable /healthz handler (DB ping only — design §10). Both the HTTP
	// listener and the MCP server mount the SAME handler.
	healthz := health.NewHandlerWithLogger(st, logger.With("component", "health"))
	metricsSrv := metrics.NewTakuhai(version, commit, st)
	ingester := ingest.New(st, metricsSrv)
	crawls, err := newCrawlRunner(cfg, ingester, metricsSrv, logger.With("component", "crawler"))
	if err != nil {
		return err
	}

	// The consumer-only MCP server (list_releases / get_release / resolve_magnets). Its Handler() serves
	// /mcp + /healthz.
	mcpSrv := mcp.NewServerWithMetricsAndLogger(st, healthz, metricsSrv, logger.With("component", "mcp"))

	logger.Info("takuhai starting",
		"version", version,
		"addr", cfg.Addr,
		"dmhy_enabled", cfg.Sources.DMHY.Enabled,
		"nyaa_enabled", cfg.Sources.Nyaa.Enabled,
	)

	return runHTTP(ctx, logger, cfg.Addr, st, mcpSrv, healthz, metricsSrv, crawls)
}

// runHTTP mounts every HTTP route — /ingest (push), /queue/* + /submit (match loop), /mcp +
// /healthz (consumer + health) — on one listener.
func runHTTP(
	ctx context.Context,
	logger *slog.Logger,
	addr string,
	st *postgres.Store,
	mcpSrv *mcp.Server,
	healthz http.Handler,
	metricsSrv *metrics.Takuhai,
	crawls *crawlrunner.Runner,
) error {
	mux := http.NewServeMux()
	// The consumer /mcp endpoint + /healthz (the MCP server owns this mux).
	mux.Handle("/mcp", mcpSrv.Handler())
	mux.Handle("/healthz", healthz)
	mux.Handle("/metrics", metricsSrv.Handler())
	// The REST push-ingestion and match-loop surfaces.
	restAPI := rest.NewWithMetricsAndLogger(st, metricsSrv, logger.With("component", "rest"))
	mux.Handle("/ingest", restAPI)
	mux.Handle("/magnets/", restAPI)
	mux.Handle("/releases/", restAPI)
	mux.Handle("/queue/", restAPI)
	mux.Handle("/submit", restAPI)

	srv := &http.Server{Addr: addr, Handler: logHTTP(logger, metricsSrv.HTTP, metricsSrv.HTTP.Wrap(mux))}

	// Bind SYNCHRONOUSLY so a failed bind (e.g. the port is already in use) fails fast:
	// run() returns the error promptly with a non-zero exit instead of leaving a process
	// "up" but serving nothing until SIGTERM (F16). Only after the listener is accepting
	// do we hand off to the background serve loop.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind %s: %w", addr, err)
	}
	logger.Info("takuhai listening", "addr", ln.Addr().String())

	crawlCtx, cancelCrawls := context.WithCancel(ctx)
	var crawlsDone chan struct{}
	if crawls != nil {
		crawlsDone = make(chan struct{})
		go func() {
			crawls.Run(crawlCtx)
			close(crawlsDone)
		}()
	}
	defer func() {
		cancelCrawls()
		if crawlsDone != nil {
			<-crawlsDone
		}
	}()

	// Serve in the background. serveErr carries the loop's single terminal error —
	// ErrServerClosed after a clean Shutdown, or the fail-fast cause if the listener
	// dies on its own.
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		logger.Info("takuhai shutting down")
		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), drainTimeout)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			logger.Warn("graceful shutdown timed out", "err", err)
			_ = srv.Close()
		}
		if err := <-serveErr; err != nil && err != http.ErrServerClosed {
			logger.Error("server stopped with error", "err", err)
			return err
		}
		logger.Info("takuhai stopped")
		return nil
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server stopped with error", "err", err)
			return err
		}
		return nil
	}
}

func logHTTP(logger *slog.Logger, routes interface{ Route(string) string }, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		captured := httpsnoop.CaptureMetrics(next, w, r)
		path := routes.Route(r.URL.Path)
		if path == "/healthz" || path == "/metrics" {
			return
		}
		logger.InfoContext(r.Context(), "http request completed",
			"component", "http",
			"method", r.Method,
			"path", path,
			"status", captured.Code,
			"duration_ms", time.Since(start).Milliseconds(),
			"response_bytes", captured.Written,
		)
	})
}

// drainTimeout bounds the graceful HTTP shutdown. A consumer holding an open /mcp
// standalone SSE GET stream blocks server-side until its request context is cancelled,
// which http.Server.Shutdown does NOT do — so an unbounded Shutdown would hang the whole
// drain forever on a steady-state SIGTERM. The deadline caps in-flight wait; on expiry
// srv.Close force-closes lingering connections so shutdown can finish.
const drainTimeout = 10 * time.Second

// runMigrations brings the target database to head over a short-lived database/sql
// handle (the embedded goose runner works over database/sql; the pgx/v5 stdlib driver
// bridges the pgx connection string). It is idempotent and closes the handle before
// the service opens its own pool.
func runMigrations(ctx context.Context, databaseURL string) error {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse database url: %w", err)
	}
	sqlDB := stdlib.OpenDB(*cfg)
	defer sqlDB.Close()
	return dbmigrations.Run(ctx, sqlDB)
}

func parseLogLevel(s string) (slog.Level, error) {
	switch s {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q", s)
	}
}

const crawlPageSize = 200

func newCrawlRunner(
	cfg config.Config,
	ingester *ingest.Processor,
	metricsSrv *metrics.Takuhai,
	logger *slog.Logger,
) (*crawlrunner.Runner, error) {
	var jobs []crawlrunner.Job
	if source := cfg.Sources.DMHY; source.Enabled {
		category, err := strconv.Atoi(source.Category)
		if err != nil {
			return nil, fmt.Errorf("parse DMHY category: %w", err)
		}
		crawler := dmhy.NewHTTPCrawler(source.URL, category, source.MaxRPS, source.CacheTTL)
		jobs = append(jobs, crawlrunner.Job{
			Source:   rawpost.SourceDMHY,
			Interval: source.Interval,
			Timeout:  source.Timeout,
			Crawl: func(ctx context.Context) ([]rawpost.RawPost, error) {
				return crawler.Crawl(ctx, crawlPageSize)
			},
		})
	}
	if source := cfg.Sources.Nyaa; source.Enabled {
		crawler := nyaa.NewHTTPCrawler(source.URL, source.Query, source.Category, source.Filter, source.MaxRPS)
		jobs = append(jobs, crawlrunner.Job{
			Source:   rawpost.SourceNyaa,
			Interval: source.Interval,
			Timeout:  source.Timeout,
			Crawl: func(ctx context.Context) ([]rawpost.RawPost, error) {
				return crawler.Crawl(ctx, crawlPageSize)
			},
		})
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return &crawlrunner.Runner{
		Jobs:    jobs,
		Ingest:  ingester.Batch,
		Metrics: metricsSrv,
		Logger:  logger,
	}, nil
}

func loadConfig() (cfg config.Config, showVersion bool, err error) {
	configPath := flag.String("config", config.DefaultPath, "TOML configuration file")
	show := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if flag.NArg() != 0 {
		return config.Config{}, false, fmt.Errorf("unexpected positional arguments: %v", flag.Args())
	}
	if *show {
		return config.Config{}, true, nil
	}

	cfg, err = config.Load(*configPath, os.Getenv("KURA_RELEASES_DATABASE_URL"))
	return cfg, false, err
}

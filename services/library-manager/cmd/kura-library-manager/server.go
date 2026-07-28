package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/wyvernzora/kura/services/library-manager/internal/config"
	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
	restserver "github.com/wyvernzora/kura/services/library-manager/internal/server/rest"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/sweep"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

type serverOptions struct {
	Config              string
	UseTestStubs        bool
	StubProviderFixture string
}

func runServer(
	parent context.Context,
	opts serverOptions,
	getenv func(string) string,
	stderr io.Writer,
) error {
	cfg, err := config.Load(opts.Config)
	if err != nil {
		return err
	}
	if err := configureUmask(cfg.Server.Umask); err != nil {
		return err
	}

	logger := newServerLogger(stderr, cfg.Server.LogLevel)
	// Bind as the process default so package-level slog calls flow through
	// the same handler + level as the explicit deps.Logger plumbing.
	slog.SetDefault(logger)

	deps, registry, watch, err := buildServeDeps(parent, getenv, cfg, logger)
	if err != nil {
		logger.Error("server bootstrap failed", "err", err)
		return err
	}
	deps, err = applyTestStubs(deps, opts)
	if err != nil {
		logger.Error("apply test stubs failed", "err", err)
		return err
	}

	// Manual signal wiring (vs signal.NotifyContext) so the signal name
	// can be logged at the moment it arrives — before transports start
	// draining. Goroutine cancels ctx on first signal; subsequent
	// signals are ignored (kernel default would force-kill).
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go runShutdownSignalLoop(ctx, sigCh, cancel, logger)

	deps.Index.Watch(ctx, watch)

	var restSrv *restserver.Server
	if cfg.Server.RESTAddr != "" {
		restSrv = restserver.NewServer(restserver.Deps{
			Workflow:       deps,
			Logger:         logger,
			AllowedOrigins: cfg.Server.RESTCORSOrigins,
			Version:        Version,
		})
	}

	logger.Info("kura serve starting",
		"version", Version,
		"libRoot", deps.LibRoot,
		"transports", serverTransports(cfg.Server),
	)

	runErr := launchServerTransports(ctx, cfg, restSrv, deps, logger)
	return finishServerShutdown(registry, logger, runErr, cfg.Server.ShutdownTimeout)
}

// runShutdownSignalLoop blocks on either the SIGINT/SIGTERM channel
// or ctx cancellation. On signal, logs the signal name and cancels
// ctx so the transport errgroup unwinds. Subsequent signals are
// ignored (kernel default would force-kill).
func runShutdownSignalLoop(ctx context.Context, sigCh <-chan os.Signal, cancel context.CancelFunc, logger *slog.Logger) {
	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received, draining", "signal", sig.String())
		cancel()
	case <-ctx.Done():
	}
}

// launchServerTransports starts the REST transport plus the
// unconditional sweep loop in their own errgroup goroutines, then
// blocks until the first one returns. The returned error is the
// errgroup's collected error (nil on clean shutdown).
func launchServerTransports(
	ctx context.Context,
	cfg config.Config,
	restSrv *restserver.Server,
	deps workflow.Deps,
	logger *slog.Logger,
) error {
	g, gctx := errgroup.WithContext(ctx)
	if restSrv != nil {
		addr := cfg.Server.RESTAddr
		opts := restserver.ServeOptions{
			PortFile: cfg.Server.RESTPortFile,
		}
		g.Go(func() error { return restserver.Serve(gctx, addr, opts, restSrv) })
	}
	g.Go(func() error {
		return sweep.Run(gctx, deps.LibRoot, sweep.Config{
			Interval:     cfg.Sweep.Interval,
			LogRetention: time.Duration(cfg.Sweep.LogRetentionDays) * 24 * time.Hour,
			Registry:     deps.Jobs,
		}, logger)
	})
	g.Go(func() error {
		runStartupRecoverySweep(gctx, deps, logger)
		return nil
	})
	return g.Wait()
}

// runStartupRecoverySweep clears stale same-host CAS claims left
// behind by a previous server instance that died mid-`reconcile apply`
// (OOMKill, eviction, rolling update). Without this, the next pod
// inherits the dead claim and every subsequent apply on that series
// surfaces BusyError until an operator manually runs
// `kura reconcile recover`.
//
// Blocks on index readiness, then runs once. Boot must not crash on a
// recovery error — all failures are logged and swallowed. Cross-host
// or genuinely-live claims are left alone; surfacing them in the log
// hands the decision to the operator.
func runStartupRecoverySweep(ctx context.Context, deps workflow.Deps, logger *slog.Logger) {
	if err := deps.Index.WaitReady(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Warn("startup recovery skipped: index never ready", "err", err)
		}
		return
	}
	out := workflow.RecoverStaleClaims(ctx, deps)
	if out.Scanned == 0 {
		return
	}
	for _, r := range out.Cleared {
		holder := r.PriorHolder
		logger.Info("startup recovery cleared stale claim",
			"ref", r.Ref.String(),
			"priorHolderOp", holder.Op,
			"priorHolderHost", holder.Host,
			"priorHolderPID", holder.PID,
		)
	}
	for _, b := range out.Busy {
		logger.Info("startup recovery skipped live claim",
			"scope", b.Scope,
			"holderOp", b.Holder.Op,
			"holderHost", b.Holder.Host,
			"holderPID", b.Holder.PID,
		)
	}
	logger.Info("startup recovery sweep complete",
		"scanned", out.Scanned,
		"cleared", len(out.Cleared),
		"busy", len(out.Busy),
	)
}

// finishServerShutdown drains the jobs registry, logs the outcome,
// and maps runErr to the process exit code. Treats context.Canceled
// as a clean shutdown (the signal goroutine fires it).
func finishServerShutdown(
	registry *jobs.Registry,
	logger *slog.Logger,
	runErr error,
	grace time.Duration,
) error {
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

// serverTransports returns the configured transport names for the boot log.
func serverTransports(cfg config.Server) []string {
	var out []string
	if cfg.RESTAddr != "" {
		out = append(out, "rest="+cfg.RESTAddr)
	}
	return out
}

// buildServeDeps constructs the workflow deps with the server serializer plus
// a configured long-lived jobs registry. Returns the
// WatchConfig the caller should pass to deps.Index.Watch under the
// signal-cancellable ctx.
//
// The supplied logger is bound to the jobs registry so job lifecycle
// events ("job submitted", "job terminal", "reaper evicted") flow
// into the same structured log stream as the boot/transport events.
func buildServeDeps(
	ctx context.Context,
	getenv func(string) string,
	cfg config.Config,
	logger *slog.Logger,
) (workflow.Deps, *jobs.Registry, indexfile.WatchConfig, error) {
	// Async index path: any cold-start rebuild proceeds in the
	// background. kura_list returns server_not_ready until the rebuild
	// completes; transports come up immediately.
	coordinator := coord.NewMCPCoordinator()
	deps, err := buildDepsAsyncIndex(ctx, getenv, cfg, coordinator, logger)
	if err != nil {
		return workflow.Deps{}, nil, indexfile.WatchConfig{}, err
	}

	// Inbox is mandatory at server startup. CLI invocations never touch
	// inbox locally; they delegate to the server, so they don't validate here.
	inboxRoot := cfg.Library.Inbox
	if err := validateInboxRoot(inboxRoot); err != nil {
		return workflow.Deps{}, nil, indexfile.WatchConfig{}, err
	}
	if err := validateRootsDisjoint(deps.LibRoot, inboxRoot); err != nil {
		return workflow.Deps{}, nil, indexfile.WatchConfig{}, err
	}
	deps.InboxRoot = inboxRoot

	registry := jobs.NewRegistry(ctx, jobs.Config{
		JobTimeout:     cfg.Jobs.Timeout,
		Retention:      cfg.Jobs.Retention,
		ReaperInterval: cfg.Jobs.ReaperInterval,
		LibRoot:        deps.LibRoot,
	}, logger)
	deps.Jobs = registry
	deps.Logger = logger

	watch := indexfile.WatchConfig{
		ProbeInterval:   cfg.Index.ProbeInterval,
		RebuildInterval: cfg.Index.RebuildInterval,
		LibRootDebounce: cfg.Index.RootDebounce,
	}
	return deps, registry, watch, nil
}

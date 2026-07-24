package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"

	"github.com/wyvernzora/kura/services/library-manager/internal/config"
	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
	"github.com/wyvernzora/kura/services/library-manager/internal/server/auth"
	mcpserver "github.com/wyvernzora/kura/services/library-manager/internal/server/mcp"
	restserver "github.com/wyvernzora/kura/services/library-manager/internal/server/rest"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/sweep"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

type serveCmd struct {
	Config string `name:"config" default:"/etc/kura/library-manager.toml" placeholder:"PATH" help:"Load serve settings from a strict TOML file."`

	// e2e-only flags wired in cmd_serve_e2e.go (build tag e2e_stub).
	// Hidden from --help; honored only when binary is built with
	// -tags=e2e_stub. Production binary silently ignores them.
	UseTestStubs        bool   `name:"use-test-stubs" hidden:"" help:""`
	StubProviderFixture string `name:"stub-provider-fixture" hidden:"" help:""`
}

func (cmd *serveCmd) Run(rt *runContext) error {
	cfg, err := config.Load(cmd.Config)
	if err != nil {
		return err
	}
	if err := configureUmask(cfg.Server.Umask); err != nil {
		return err
	}

	logger := newServerLogger(rt.Stderr, cfg.Server.LogLevel)
	// Bind as the process default so package-level slog calls flow through
	// the same handler + level as the explicit deps.Logger plumbing.
	slog.SetDefault(logger)

	deps, registry, watch, err := buildServeDeps(rt, cfg, logger)
	if err != nil {
		logger.Error("server bootstrap failed", "err", err)
		return err
	}
	deps, err = applyTestStubs(deps, cmd)
	if err != nil {
		logger.Error("apply test stubs failed", "err", err)
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
	go runShutdownSignalLoop(ctx, sigCh, cancel, logger)

	deps.Index.Watch(ctx, watch)

	// Applies to both REST and MCP-HTTP transports. MCP-stdio is
	// unauthenticated (process boundary).
	tokenResult, err := auth.Load(auth.Options{
		Disabled:  cfg.Auth.Disabled,
		Token:     rt.Getenv(auth.EnvLiteral),
		TokenPath: cfg.Auth.TokenPath,
	})
	if err != nil {
		logger.Error("auth token load failed", "err", err)
		return err
	}
	logTokenStatus(logger, tokenResult, cfg.Auth.TokenPath)

	server := mcpserver.NewServer(mcpserver.Deps{
		Workflow:    deps,
		Logger:      logger,
		BearerToken: tokenResult.Token,
		Version:     Version,
	})

	var restSrv *restserver.Server
	if cfg.Server.RESTAddr != "" {
		restSrv = restserver.NewServer(restserver.Deps{
			Workflow:       deps,
			Logger:         logger,
			AllowedOrigins: cfg.Server.RESTCORSOrigins,
			BearerToken:    tokenResult.Token,
			Version:        Version,
		})
	}

	logger.Info("kura serve starting",
		"version", Version,
		"libRoot", deps.LibRoot,
		"transports", serverTransports(cfg.Server),
	)

	runErr := launchServerTransports(ctx, cfg, server, restSrv, tokenResult, deps, logger)
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

// launchServerTransports starts each enabled transport (mcp-stdio,
// mcp-http, rest) plus the unconditional sweep loop in their own
// errgroup goroutines, then blocks until the first one returns. The
// returned error is the errgroup's collected error (nil on clean
// shutdown).
func launchServerTransports(
	ctx context.Context,
	cfg config.Config,
	server *sdkmcp.Server,
	restSrv *restserver.Server,
	tokenResult auth.Result,
	deps workflow.Deps,
	logger *slog.Logger,
) error {
	g, gctx := errgroup.WithContext(ctx)
	if cfg.Server.MCPStdio {
		g.Go(func() error { return mcpserver.ServeStdio(gctx, server) })
	}
	if cfg.Server.MCPHTTPAddr != "" {
		addr := cfg.Server.MCPHTTPAddr
		token := tokenResult.Token
		g.Go(func() error { return mcpserver.ServeHTTP(gctx, addr, server, token) })
	}
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

// logTokenStatus emits one structured log line describing the auth
// posture, including a copy-paste hint when a fresh token was just
// generated. Operators wiring up the CLI for the first time get the
// secret from this line plus the token file path.
func logTokenStatus(logger *slog.Logger, r auth.Result, tokenPath string) {
	switch {
	case r.Disabled:
		logger.Warn("kura serve auth disabled by config",
			"hint", "front kura with an authenticating proxy or set auth.disabled=false")
	case r.Generated:
		logger.Info("kura serve generated bearer token",
			"path", tokenPath,
			"hint", "set KURA_TOKEN="+r.Token+" on clients (or read the token file)")
	default:
		logger.Info("kura serve bearer token loaded", "source", r.Source)
	}
}

// serverTransports returns the configured transport names for the boot log.
func serverTransports(cfg config.Server) []string {
	var out []string
	if cfg.MCPStdio {
		out = append(out, "mcp-stdio")
	}
	if cfg.MCPHTTPAddr != "" {
		out = append(out, "mcp-http="+cfg.MCPHTTPAddr)
	}
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
	rt *runContext,
	cfg config.Config,
	logger *slog.Logger,
) (workflow.Deps, *jobs.Registry, indexfile.WatchConfig, error) {
	// Async index path: any cold-start rebuild proceeds in the
	// background. kura_list returns server_not_ready until the rebuild
	// completes; transports come up immediately.
	coordinator := coord.NewMCPCoordinator()
	deps, err := buildDepsAsyncIndex(rt, cfg, coordinator, logger)
	if err != nil {
		return workflow.Deps{}, nil, indexfile.WatchConfig{}, err
	}

	// Inbox is mandatory for `kura serve` (where stage / kura_inbox_list
	// run). CLI invocations never touch inbox locally; they delegate to
	// the server, so they don't validate here.
	inboxRoot := cfg.Library.Inbox
	if err := validateInboxRoot(inboxRoot); err != nil {
		return workflow.Deps{}, nil, indexfile.WatchConfig{}, err
	}
	if err := validateRootsDisjoint(deps.LibRoot, inboxRoot); err != nil {
		return workflow.Deps{}, nil, indexfile.WatchConfig{}, err
	}
	deps.InboxRoot = inboxRoot

	registry := jobs.NewRegistry(rt.Context, jobs.Config{
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

package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/server/auth"
	mcpserver "github.com/wyvernzora/kura/internal/server/mcp"
	restserver "github.com/wyvernzora/kura/internal/server/rest"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/sweep"
	"github.com/wyvernzora/kura/internal/workflow"
)

// defaultShutdownTimeout caps how long cmd_serve waits for the jobs
// registry to drain after the transport ctx is cancelled.
const defaultShutdownTimeout = 10 * time.Second

type serveCmd struct {
	MCPStdio        bool     `name:"mcp-stdio" help:"Run MCP transport over stdio (Claude Desktop, mcp inspector --stdio)."`
	MCPHTTP         string   `name:"mcp-http" placeholder:"ADDR" help:"Run MCP transport over streamable HTTP at the given address (e.g. ':8080' or '127.0.0.1:8080')."`
	REST            string   `name:"rest" placeholder:"ADDR" help:"Run REST transport at the given address (e.g. '127.0.0.1:8080' or ':8080'). Access requires the bearer token at /var/lib/kura/token unless KURA_DISABLE_TOKEN=1."`
	RESTCORSOrigins []string `name:"rest-cors-origin" placeholder:"ORIGIN" help:"Add an Origin to the REST CORS allow-list (repeatable, or '*' to allow any origin). Empty list disables CORS headers."`
	RESTPortFile    string   `name:"rest-port-file" placeholder:"PATH" help:"After --rest binds, atomically write the resolved port number to PATH (decimal + newline). Useful with ':0' for ephemeral binds."`

	// e2e-only flags wired in cmd_serve_e2e.go (build tag e2e_stub).
	// Hidden from --help; honored only when binary is built with
	// -tags=e2e_stub. Production binary silently ignores them.
	UseTestStubs        bool   `name:"use-test-stubs" hidden:"" help:""`
	StubProviderFixture string `name:"stub-provider-fixture" hidden:"" help:""`
}

func (cmd *serveCmd) Run(rt *runContext) error {
	if !cmd.MCPStdio && cmd.MCPHTTP == "" && cmd.REST == "" {
		return errors.New("kura serve requires at least one transport flag (--mcp-stdio, --mcp-http=ADDR, or --rest=ADDR)")
	}

	logger := newServerLogger(rt.Stderr, rt.Getenv)
	// Bind as the process default so package-level slog calls flow through
	// the same handler + level as the explicit deps.Logger plumbing.
	slog.SetDefault(logger)

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

	// Resolve the bearer token gate per the kura conventions:
	// KURA_DISABLE_TOKEN > KURA_TOKEN > /var/lib/kura/token > generate.
	// Applies to both REST and MCP-HTTP transports. MCP-stdio is
	// unauthenticated (process boundary).
	tokenResult, err := auth.Load(rt.Getenv, "")
	if err != nil {
		logger.Error("auth token load failed", "err", err)
		return err
	}
	logTokenStatus(logger, tokenResult)

	server := mcpserver.NewServer(mcpserver.Deps{
		Workflow:    deps,
		Logger:      logger,
		BearerToken: tokenResult.Token,
		Version:     Version,
	})

	var restSrv *restserver.Server
	if cmd.REST != "" {
		restSrv = restserver.NewServer(restserver.Deps{
			Workflow:       deps,
			Logger:         logger,
			AllowedOrigins: cmd.RESTCORSOrigins,
			BearerToken:    tokenResult.Token,
			Version:        Version,
		})
	}

	logger.Info("kura serve starting",
		"version", Version,
		"libRoot", deps.LibRoot,
		"transports", serverTransports(cmd),
	)

	// Print a clickable bootstrap URL for the web UI. Pre-fills the
	// bearer via ?token=... — the SPA consumes the param on first
	// load, persists into sessionStorage, and scrubs the token from
	// the URL via history.replaceState so it doesn't survive into
	// browser history. Skipped when --rest isn't enabled.
	if cmd.REST != "" {
		if uiURL := uiBootstrapURL(cmd.REST, tokenResult.Token); uiURL != "" {
			logger.Info("kura web UI ready", "url", uiURL)
		}
	}

	runErr := launchServerTransports(ctx, cmd, server, restSrv, tokenResult, deps, logger, rt)
	return finishServerShutdown(rt, registry, logger, runErr)
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
	cmd *serveCmd,
	server *sdkmcp.Server,
	restSrv *restserver.Server,
	tokenResult auth.Result,
	deps workflow.Deps,
	logger *slog.Logger,
	rt *runContext,
) error {
	g, gctx := errgroup.WithContext(ctx)
	if cmd.MCPStdio {
		g.Go(func() error { return mcpserver.ServeStdio(gctx, server) })
	}
	if cmd.MCPHTTP != "" {
		addr := cmd.MCPHTTP
		token := tokenResult.Token
		g.Go(func() error { return mcpserver.ServeHTTP(gctx, addr, server, token) })
	}
	if restSrv != nil {
		addr := cmd.REST
		opts := restserver.ServeOptions{
			PortFile: cmd.RESTPortFile,
		}
		g.Go(func() error { return restserver.Serve(gctx, addr, opts, restSrv) })
	}
	g.Go(func() error {
		return sweep.Run(gctx, deps.LibRoot, sweep.Config{
			Interval:     envDuration(rt.Getenv, "KURA_SWEEP_INTERVAL", 0),
			Jitter:       envDuration(rt.Getenv, "KURA_SWEEP_JITTER", 0),
			LogRetention: envDays(rt.Getenv, "KURA_LOG_RETENTION_DAYS", 0),
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
func finishServerShutdown(rt *runContext, registry *jobs.Registry, logger *slog.Logger, runErr error) error {
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

// logTokenStatus emits one structured log line describing the auth
// posture, including a copy-paste hint when a fresh token was just
// generated. Operators wiring up the CLI for the first time get the
// secret from this line plus the token file path.
func logTokenStatus(logger *slog.Logger, r auth.Result) {
	switch {
	case r.Disabled:
		logger.Warn("kura serve auth disabled (KURA_DISABLE_TOKEN)",
			"hint", "front kura with an authenticating proxy or unset KURA_DISABLE_TOKEN to re-enable the bearer-token gate")
	case r.Generated:
		logger.Info("kura serve generated bearer token",
			"path", auth.DefaultTokenPath,
			"hint", "set KURA_TOKEN="+r.Token+" on clients (or read the token file)")
	default:
		logger.Info("kura serve bearer token loaded", "source", r.Source)
	}
}

// uiBootstrapURL builds a click-to-open URL for the embedded web
// UI. Loopback bind addresses (`:port`, `0.0.0.0:port`, `[::]:port`)
// resolve to `127.0.0.1` so the link works from the host that ran
// `kura serve`; explicit hosts pass through unchanged. The bearer
// token, if present, is appended as `?token=...` and URL-encoded.
//
// Returns "" if `restAddr` doesn't parse — caller skips the log.
func uiBootstrapURL(restAddr, token string) string {
	host, port, err := net.SplitHostPort(restAddr)
	if err != nil {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	base := "http://" + net.JoinHostPort(host, port) + "/"
	if token == "" {
		return base
	}
	return base + "?token=" + url.QueryEscape(token)
}

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
	if cmd.REST != "" {
		out = append(out, "rest="+cmd.REST)
	}
	return out
}

// buildServeDeps constructs the workflow deps with the server serializer plus
// a long-lived jobs registry configured from KURA_JOB_* envs. Returns the
// WatchConfig the caller should pass to deps.Index.Watch under the
// signal-cancellable ctx.
//
// The supplied logger is bound to the jobs registry so job lifecycle
// events ("job submitted", "job terminal", "reaper evicted") flow
// into the same structured log stream as the boot/transport events.
func buildServeDeps(rt *runContext, logger *slog.Logger) (workflow.Deps, *jobs.Registry, indexfile.WatchConfig, error) {
	// Async index path: any cold-start rebuild proceeds in the
	// background. kura_list returns server_not_ready until the rebuild
	// completes; transports come up immediately.
	coordinator := coord.NewMCPCoordinator()
	deps, err := buildDepsAsyncIndex(rt, coordinator, logger)
	if err != nil {
		return workflow.Deps{}, nil, indexfile.WatchConfig{}, err
	}

	// Inbox is mandatory for `kura serve` (where stage / kura_inbox_list
	// run). CLI invocations never touch inbox locally; they delegate to
	// the server, so they don't validate here.
	inboxRoot := rt.Getenv("KURA_INBOX_ROOT")
	if err := validateInboxRoot(inboxRoot); err != nil {
		return workflow.Deps{}, nil, indexfile.WatchConfig{}, err
	}
	if err := validateRootsDisjoint(deps.LibRoot, inboxRoot); err != nil {
		return workflow.Deps{}, nil, indexfile.WatchConfig{}, err
	}
	deps.InboxRoot = inboxRoot

	registry := jobs.NewRegistry(rt.Context, jobs.Config{
		JobTimeout:     envDuration(rt.Getenv, "KURA_JOB_TIMEOUT", 0),
		Retention:      envDuration(rt.Getenv, "KURA_JOB_RETENTION", 30*time.Minute),
		ReaperInterval: envDuration(rt.Getenv, "KURA_JOB_REAPER_INTERVAL", 5*time.Minute),
		LibRoot:        deps.LibRoot,
	}, logger)
	deps.Jobs = registry
	deps.Logger = logger

	watch := indexfile.WatchConfig{
		ProbeInterval:   envDuration(rt.Getenv, "KURA_INDEX_PROBE_INTERVAL", 2*time.Second),
		RebuildInterval: envDuration(rt.Getenv, "KURA_INDEX_REBUILD_INTERVAL", time.Hour),
		LibRootDebounce: envDuration(rt.Getenv, "KURA_INDEX_LIBROOT_DEBOUNCE", 3*time.Second),
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

// envDays parses an integer day count from the named env var and
// converts it to time.Duration. Empty / invalid / negative inputs
// fall back to the default. Days are the user-facing unit for log
// retention; sweep internally uses Duration.
func envDays(getenv func(string) string, key string, fallback time.Duration) time.Duration {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return fallback
	}
	return time.Duration(n) * 24 * time.Hour
}

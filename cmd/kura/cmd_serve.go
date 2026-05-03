package main

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/jobs"
	mcpserver "github.com/wyvernzora/kura/internal/server/mcp"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
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

	deps, registry, watcher, err := buildServeDeps(rt)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(rt.Context, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if watcher != nil {
		watcher.Run(ctx)
	}

	server := mcpserver.NewServer(mcpserver.Deps{Workflow: deps})

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
		fmt.Fprintf(rt.Stderr, "kura serve: %d job(s) did not shut down within %s\n", stuck, grace)
	}

	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runErr
	}
	return nil
}

// buildServeDeps wraps buildDeps and swaps the in-process serializer
// (CLI no-op → MCP per-series mutex) plus a long-lived jobs registry
// configured from KURA_JOB_* envs. Constructs the index Watcher
// (KURA_INDEX_*) over the same *Index workflows mutate so external
// peer writes get picked up. The CLI registry from buildDeps is
// discarded; it never received a Submit so no goroutines leak.
func buildServeDeps(rt *runContext) (workflow.Deps, *jobs.Registry, *indexfile.Watcher, error) {
	deps, err := buildDeps(rt)
	if err != nil {
		return workflow.Deps{}, nil, nil, err
	}
	attempts := envInt(rt.Getenv, "KURA_CONFLICT_RETRIES", 1) + 1
	deps.Coordinator = coord.NewMCPCoordinator(coord.MaxAttempts(attempts))

	registry := jobs.NewRegistry(rt.Context, jobs.Config{
		JobTimeout:     envDuration(rt.Getenv, "KURA_JOB_TIMEOUT", 0),
		Retention:      envDuration(rt.Getenv, "KURA_JOB_RETENTION", 30*time.Minute),
		ReaperInterval: envDuration(rt.Getenv, "KURA_JOB_REAPER_INTERVAL", 5*time.Minute),
	}, nil)
	deps.Jobs = registry

	libRoot := deps.LibRoot
	reader := func(_ context.Context, ref refs.Series) (refs.Metadata, error) {
		return seriesfile.ReadMetadataRef(libRoot, ref)
	}
	watcher := indexfile.NewWatcher(deps.Index, reader, indexfile.WatcherConfig{
		ProbeInterval:   envDuration(rt.Getenv, "KURA_INDEX_PROBE_INTERVAL", 2*time.Second),
		RefreshInterval: envDuration(rt.Getenv, "KURA_INDEX_REFRESH_INTERVAL", 5*time.Minute),
		RebuildInterval: envDuration(rt.Getenv, "KURA_INDEX_REBUILD_INTERVAL", time.Hour),
	}, nil)
	return deps, registry, watcher, nil
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

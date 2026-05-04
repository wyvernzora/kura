package main

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/wyvernzora/kura/internal/config"
	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/workflow"
)

// buildSourceFromFlags constructs the metadata source from the global CLI
// flags. Used by run.go to seed the lazy provider.WithSource builder.
func buildSourceFromFlags(rt *runContext, flags *cli) (provider.Source, error) {
	return config.BuildMetadataSource(config.MetadataSourceOptions{
		TVDBBaseURL: flags.TVDBBaseURL,
		Getenv:      rt.Getenv,
	})
}

// buildDeps constructs the workflow.Deps used by every workflow call.
// CLI commands need a ready index before any workflow runs, so this
// blocks on any in-flight rebuild via WaitReady. buildServeDeps uses
// buildDepsAsyncIndex to skip the wait and let transports come up
// while the rebuild proceeds in the background.
//
// The metadata provider is wrapped in a sync.Once factory so local-only
// commands run without KURA_TVDB_KEY; provider-needing workflows surface
// the missing-key error only when actually required.
func buildDeps(rt *runContext) (workflow.Deps, error) {
	deps, err := buildDepsAsyncIndex(rt)
	if err != nil {
		return deps, err
	}
	if err := deps.Index.WaitReady(rt.Context); err != nil {
		return workflow.Deps{}, err
	}
	return deps, nil
}

// buildDepsAsyncIndex constructs Deps without blocking on the index
// rebuild. Used by serve to surface KindServerNotReady to early
// requests instead of delaying transport startup.
func buildDepsAsyncIndex(rt *runContext) (workflow.Deps, error) {
	libRoot := rt.Getenv("KURA_LIBRARY_ROOT")
	if err := validateLibraryRoot(libRoot); err != nil {
		return workflow.Deps{}, err
	}
	index, err := loadOrRebuildIndex(rt.Context, libRoot)
	if err != nil {
		return workflow.Deps{}, err
	}
	inspector := mediainfo.New()
	if cmd := rt.Getenv("KURA_MEDIAINFO_COMMAND"); cmd != "" {
		inspector.Command = cmd
	}
	provider := workflow.NewProviderFactory(func() (provider.Source, error) {
		return buildSourceFromFlags(rt, rt.flags)
	})
	hostName, err := os.Hostname()
	if err != nil {
		hostName = "unknown"
	}
	attempts := envInt(rt.Getenv, "KURA_CONFLICT_RETRIES", 1) + 1
	coordImpl := coord.NewCLICoordinator(coord.MaxAttempts(attempts))
	// CLI registry: parent ctx is the CLI invocation's ctx. Goroutines
	// die with the process; no Shutdown needed. Reaper / retention
	// disabled — invocation is short-lived, nothing accumulates.
	registry := jobs.NewRegistry(rt.Context, jobs.Config{}, nil)
	return workflow.Deps{
		LibRoot:     libRoot,
		Index:       index,
		Coordinator: coordImpl,
		HostName:    hostName,
		Provider:    provider,
		Inspector:   inspector,
		Now:         time.Now,
		Jobs:        registry,
	}, nil
}

func envInt(getenv func(string) string, key string, fallback int) int {
	value := getenv(key)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func validateLibraryRoot(root string) error {
	if root == "" {
		return errors.New("KURA_LIBRARY_ROOT is required")
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return workflow.ErrLibraryRootNotFound
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return workflow.ErrLibraryRootNotDirectory
	}
	return nil
}

// loadOrRebuildIndex returns an Index for libRoot. If index.jsonl is
// present, it's loaded synchronously and returned ready. Otherwise a
// fresh Index is returned with a background rebuild already triggered;
// callers that need the rebuild to finish before returning (CLI) call
// idx.WaitReady. After a successful path either way, the legacy
// index.tsv is removed best-effort so the artifact doesn't linger.
func loadOrRebuildIndex(ctx context.Context, libRoot string) (*indexfile.Index, error) {
	index, err := indexfile.Load(libRoot)
	if errors.Is(err, indexfile.ErrNotFound) {
		index = indexfile.New(libRoot)
		index.TriggerRebuild(ctx, libRoot, indexfile.BuildRow, coord.NewMutator("bootstrap"))
	} else if err != nil {
		return nil, err
	}
	_ = os.Remove(paths.LegacyIndexFile(libRoot))
	return index, nil
}

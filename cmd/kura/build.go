package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	coordImpl := coord.NewCLICoordinator()
	// CLI registry: parent ctx is the CLI invocation's ctx. Goroutines
	// die with the process; no Shutdown needed. Reaper / retention
	// disabled — invocation is short-lived, nothing accumulates.
	registry := jobs.NewRegistry(rt.Context, jobs.Config{}, nil)
	prefs, err := config.ParsePreferredLanguages(rt.Getenv("KURA_PREFERRED_LANGUAGES"))
	if err != nil {
		return workflow.Deps{}, err
	}
	return workflow.Deps{
		LibRoot:            libRoot,
		Index:              index,
		Coordinator:        coordImpl,
		HostName:           hostName,
		Provider:           provider,
		Inspector:          inspector,
		Now:                time.Now,
		Jobs:               registry,
		PreferredLanguages: prefs.Tags(),
	}, nil
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

// validateInboxRoot enforces the same shape as validateLibraryRoot:
// non-empty, exists, is a directory. Used by `kura serve` startup; CLI
// commands don't call this because all inbox interaction goes through
// the server's REST surface.
func validateInboxRoot(root string) error {
	if root == "" {
		return errors.New("KURA_INBOX_ROOT is required")
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("KURA_INBOX_ROOT does not exist")
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("KURA_INBOX_ROOT is not a directory")
	}
	return nil
}

// validateRootsDisjoint refuses configurations where library root and
// inbox root overlap (one is the other, or one contains the other).
// Resolves via filepath.Abs + EvalSymlinks before the prefix check so
// macOS /var → /private/var aliasing doesn't create false positives.
func validateRootsDisjoint(libRoot, inboxRoot string) error {
	lib, err := canonicalRoot(libRoot)
	if err != nil {
		return err
	}
	inbox, err := canonicalRoot(inboxRoot)
	if err != nil {
		return err
	}
	if lib == inbox {
		return errors.New("KURA_LIBRARY_ROOT and KURA_INBOX_ROOT must be distinct paths")
	}
	if hasPathPrefix(inbox, lib) {
		return errors.New("KURA_INBOX_ROOT must not live inside KURA_LIBRARY_ROOT")
	}
	if hasPathPrefix(lib, inbox) {
		return errors.New("KURA_LIBRARY_ROOT must not live inside KURA_INBOX_ROOT")
	}
	return nil
}

func canonicalRoot(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs, nil
	}
	return resolved, nil
}

func hasPathPrefix(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// loadOrRebuildIndex returns an Index for libRoot. If index.jsonl is
// present and parseable at the current SchemaVersion, it's loaded
// synchronously and returned ready. If absent OR the on-disk schema
// doesn't match this build, a fresh Index is returned with a
// background rebuild already triggered; callers that need the rebuild
// to finish before returning (CLI) call idx.WaitReady. After a
// successful path either way, the legacy index.tsv is removed
// best-effort so the artifact doesn't linger.
//
// Schema mismatch handling: the stale file is removed before
// triggering the rebuild so the rebuild's SaveCAS(expected="")
// create-path succeeds against an empty slot. Without the delete
// the create-path would conflict with the existing (wrong-schema)
// file and the rebuild would no-op.
func loadOrRebuildIndex(ctx context.Context, libRoot string) (*indexfile.Index, error) {
	index, err := indexfile.Load(libRoot)
	switch {
	case errors.Is(err, indexfile.ErrNotFound):
		index = indexfile.New(libRoot)
		index.TriggerRebuild(ctx, libRoot, indexfile.BuildRow, coord.NewMutator("rebuild_cold"))
	case errors.Is(err, indexfile.ErrSchemaMismatch):
		if rmErr := os.Remove(paths.IndexFile(libRoot)); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return nil, rmErr
		}
		index = indexfile.New(libRoot)
		index.TriggerRebuild(ctx, libRoot, indexfile.BuildRow, coord.NewMutator("rebuild_corruption"))
	case err != nil:
		return nil, err
	}
	_ = os.Remove(paths.LegacyIndexFile(libRoot))
	return index, nil
}

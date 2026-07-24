package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/config"
	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
	"github.com/wyvernzora/kura/services/library-manager/internal/mediainfo"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

func buildMetadataSource(rt *runContext, cfg config.Config) (provider.Source, error) {
	return config.BuildMetadataSource(config.MetadataSourceOptions{
		APIKey:             rt.Getenv("KURA_TVDB_KEY"),
		TVDBURL:            cfg.Metadata.TVDBURL,
		PreferredLanguages: cfg.Metadata.PreferredLanguages,
	})
}

// buildDepsAsyncIndex constructs Deps without blocking on the index
// rebuild. Used by serve to surface KindServerNotReady to early
// requests instead of delaying transport startup.
func buildDepsAsyncIndex(
	rt *runContext,
	cfg config.Config,
	coordinator coord.Coordinator,
	logger *slog.Logger,
) (workflow.Deps, error) {
	libRoot := cfg.Library.Root
	if err := validateLibraryRoot(libRoot); err != nil {
		return workflow.Deps{}, err
	}
	rowBuildOptions := indexfile.DefaultBuildOptions()
	rowBuildOptions.AiringTailDays = cfg.Library.AiringTailDays
	index, err := loadOrRebuildIndex(rt.Context, libRoot, rowBuildOptions, coordinator.WithIndex, logger)
	if err != nil {
		return workflow.Deps{}, err
	}
	inspector := mediainfo.New()
	inspector.Command = cfg.Metadata.MediaInfoCommand
	provider := workflow.NewProviderFactory(func() (provider.Source, error) {
		return buildMetadataSource(rt, cfg)
	})
	// Placeholder registry: buildServeDeps replaces it with the
	// configured long-lived registry before transports start.
	registry := jobs.NewRegistry(rt.Context, jobs.Config{}, nil)
	return workflow.Deps{
		LibRoot:            libRoot,
		Index:              index,
		Coordinator:        coordinator,
		Provider:           provider,
		Inspector:          inspector,
		Now:                time.Now,
		Jobs:               registry,
		PreferredLanguages: cfg.Metadata.PreferredLanguages,
		RowBuildOptions:    &rowBuildOptions,
		ConflictAttempts:   cfg.Coordination.ConflictRetries + 1,
	}, nil
}

func validateLibraryRoot(root string) error {
	if root == "" {
		return errors.New("library.root is required")
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
		return errors.New("library.inbox is required")
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("library.inbox does not exist")
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("library.inbox is not a directory")
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
		return errors.New("library.root and library.inbox must be distinct paths")
	}
	if hasPathPrefix(inbox, lib) {
		return errors.New("library.inbox must not live inside library.root")
	}
	if hasPathPrefix(lib, inbox) {
		return errors.New("library.root must not live inside library.inbox")
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
// present and parseable at the current SchemaVersion, it is loaded
// synchronously and returned ready. If absent or unreadable, a fresh Index is
// returned with a background rebuild already triggered; early list requests
// see server_not_ready until the rebuild publishes entries.
func loadOrRebuildIndex(ctx context.Context, libRoot string, opts indexfile.BuildOptions, guard indexfile.GuardFunc, logger *slog.Logger) (*indexfile.Index, error) {
	cfg := indexfile.Config{BuildOptions: opts, Guard: guard}
	if logger != nil {
		// Assign inside a nil check: a nil *slog.Logger stored in the
		// interface field would pass indexfile's nil test and panic.
		cfg.Logger = logger
	}
	index, err := indexfile.Load(libRoot, cfg)
	switch {
	case errors.Is(err, indexfile.ErrNotFound):
		index = indexfile.New(libRoot, cfg)
		index.TriggerRebuild(ctx, "rebuild_cold")
	case err != nil:
		slog.Warn("indexfile: load failed, triggering rebuild", "err", err)
		index = indexfile.New(libRoot, cfg)
		index.TriggerRebuild(ctx, "rebuild_corruption")
	}
	_ = os.Remove(paths.LegacyIndexFile(libRoot))
	return index, nil
}

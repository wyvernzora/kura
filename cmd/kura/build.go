package main

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/wyvernzora/kura/internal/config"
	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
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
// The metadata provider is wrapped in a sync.Once factory so local-only
// commands run without KURA_TVDB_KEY; provider-needing workflows surface
// the missing-key error only when actually required.
func buildDeps(rt *runContext) (workflow.Deps, error) {
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
	return workflow.Deps{
		LibRoot:     libRoot,
		Index:       index,
		Coordinator: coordImpl,
		HostName:    hostName,
		Provider:    provider,
		Inspector:   inspector,
		Now:         time.Now,
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

func loadOrRebuildIndex(ctx context.Context, libRoot string) (*indexfile.Index, error) {
	index, err := indexfile.Load(libRoot)
	if errors.Is(err, indexfile.ErrNotFound) {
		return indexfile.Rebuild(ctx, libRoot, func(_ context.Context, ref refs.Series) (refs.Metadata, error) {
			return seriesfile.ReadMetadataRef(libRoot, ref)
		})
	}
	return index, err
}

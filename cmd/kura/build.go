package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/wyvernzora/kura/internal/config"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/media/mediainfo"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/series"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/workflow"
)

// buildSourceFromFlags constructs the metadata source from the global CLI
// flags. Used by run.go to seed the lazy metadata.WithSource builder.
func buildSourceFromFlags(rt *runContext, flags *cli) (metadata.Source, error) {
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
	provider := workflow.NewProviderFactory(func() (metadata.Source, error) {
		return buildSourceFromFlags(rt, rt.flags)
	})
	return workflow.Deps{
		LibRoot:   libRoot,
		Index:     index,
		Provider:  provider,
		Inspector: inspector,
		Now:       time.Now,
	}, nil
}

func validateLibraryRoot(root string) error {
	if root == "" {
		return errors.New("KURA_LIBRARY_ROOT is required")
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return library.ErrRootNotFound
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return library.ErrRootNotDirectory
	}
	return nil
}

func loadOrRebuildIndex(ctx context.Context, libRoot string) (*indexfile.Index, error) {
	index, err := indexfile.Load(libRoot)
	if errors.Is(err, indexfile.ErrNotFound) {
		return indexfile.Rebuild(ctx, libRoot, func(_ context.Context, ref refs.Series) (refs.Metadata, error) {
			return series.ReadMetadataRef(libRoot, ref)
		})
	}
	return index, err
}

package main

import (
	"context"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

type reindexCmd struct{}

func (cmd *reindexCmd) Run(rt *runContext) error {
	libRoot := rt.Getenv("KURA_LIBRARY_ROOT")
	if err := validateLibraryRoot(libRoot); err != nil {
		return err
	}
	index, err := indexfile.Rebuild(rt.Context, libRoot, func(_ context.Context, ref refs.Series) (refs.Metadata, error) {
		return seriesfile.ReadMetadataRef(libRoot, ref)
	})
	if err != nil {
		return err
	}
	return index.Save()
}

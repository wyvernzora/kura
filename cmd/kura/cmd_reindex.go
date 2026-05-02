package main

import (
	"context"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

type reindexCmd struct{}

func (cmd *reindexCmd) Run(rt *runContext) error {
	root, err := library.ParseRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}
	index, err := indexfile.Rebuild(rt.Context, root.Path(), func(_ context.Context, ref refs.Series) (refs.Metadata, error) {
		return seriesfile.ReadMetadataRef(root.Path(), ref)
	})
	if err != nil {
		return err
	}
	return index.Save()
}

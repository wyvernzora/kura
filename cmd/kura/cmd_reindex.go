package main

import (
	"context"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/series"
)

type reindexCmd struct{}

func (cmd *reindexCmd) Run(rt *runContext) error {
	root, err := library.ParseRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}
	index, err := library.RebuildIndex(rt.Context, root, func(_ context.Context, ref refs.Series) (refs.Metadata, error) {
		return series.ReadMetadataRef(root.Path(), ref)
	})
	if err != nil {
		return err
	}
	return index.Save()
}

package kura

import (
	"context"
	"errors"

	"github.com/wyvernzora/kura/internal/refs"
	seriespkg "github.com/wyvernzora/kura/internal/series"
)

func (l *Library) Import(ctx context.Context, in ImportInput) (*Series, error) {
	if in.Ref == "" {
		return nil, errors.New("kura: series ref is required")
	}
	if in.MetadataRef == "" {
		return nil, errors.New("kura: metadata ref is required")
	}
	ref, err := refs.ParseSeries(in.Ref.String())
	if err != nil {
		return nil, err
	}
	metadataRef, err := refs.ParseMetadata(in.MetadataRef.String())
	if err != nil {
		return nil, err
	}
	handle, err := l.series.Import(ctx, seriespkg.ImportInput{Metadata: metadataRef, Ref: ref})
	if err != nil {
		return nil, normalizeSeriesLibraryError(err)
	}
	model, err := handle.Load()
	if err != nil {
		return nil, err
	}
	return newSeriesModel(l, SeriesRef(handle.Ref()), model), nil
}

package kura

import (
	"context"
	"errors"

	"github.com/wyvernzora/kura/internal/refs"
	seriespkg "github.com/wyvernzora/kura/internal/series"
)

func (l *Library) Add(ctx context.Context, in AddInput) (*Series, error) {
	if in.MetadataRef == "" {
		return nil, errors.New("kura: metadata ref is required")
	}
	metadataRef, err := refs.ParseMetadata(in.MetadataRef.String())
	if err != nil {
		return nil, err
	}
	handle, err := l.series.Add(ctx, seriespkg.AddInput{Metadata: metadataRef, Ref: refs.Series(in.Ref)})
	if err != nil {
		return nil, normalizeSeriesLibraryError(err)
	}
	model, err := handle.Load()
	if err != nil {
		return nil, err
	}
	return newSeriesModel(l, SeriesRef(handle.Ref()), model), nil
}

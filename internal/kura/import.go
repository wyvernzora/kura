package kura

import (
	"context"
	"errors"

	librarypkg "github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series"
)

func (l *Library) Import(ctx context.Context, in ImportInput) (series.Handle, error) {
	if in.Ref == "" {
		return series.Handle{}, errors.New("kura: series ref is required")
	}
	if in.MetadataRef == "" {
		return series.Handle{}, errors.New("kura: metadata ref is required")
	}
	ref, err := refs.ParseSeries(in.Ref.String())
	if err != nil {
		return series.Handle{}, err
	}
	metadataRef, err := refs.ParseMetadata(in.MetadataRef.String())
	if err != nil {
		return series.Handle{}, err
	}
	handle, err := l.series.Import(ctx, librarypkg.ImportInput{Metadata: metadataRef, Ref: ref})
	if err != nil {
		return series.Handle{}, err
	}
	return handle, nil
}

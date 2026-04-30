package kura

import (
	"context"
	"errors"

	librarypkg "github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series"
)

func (l *Library) Add(ctx context.Context, in AddInput) (series.Handle, error) {
	if in.MetadataRef == "" {
		return series.Handle{}, errors.New("kura: metadata ref is required")
	}
	metadataRef, err := refs.ParseMetadata(in.MetadataRef.String())
	if err != nil {
		return series.Handle{}, err
	}
	handle, err := l.series.Add(ctx, librarypkg.AddInput{Metadata: metadataRef, Ref: in.Ref})
	if err != nil {
		return series.Handle{}, err
	}
	return handle, nil
}

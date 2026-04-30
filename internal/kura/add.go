package kura

import (
	"context"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/internal/domain"
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

func normalizeInitMetadataError(err error, ref domain.MetadataRef) error {
	if ref.Source() != "tvdb" {
		return UnsupportedMetadataSourceError{Source: ref.Source()}
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("kura: metadata ref %s is invalid", ref)
}

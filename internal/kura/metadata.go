package kura

import (
	"context"
	"errors"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/store"
)

func (l *Library) fetchMetadataSeries(ctx context.Context, ref MetadataRef) (metadata.Series, domain.MetadataRef, error) {
	parsed, err := domain.ParseMetadataRef(string(ref))
	if err != nil {
		return metadata.Series{}, domain.MetadataRef{}, err
	}
	if parsed.Source() != "tvdb" {
		return metadata.Series{}, domain.MetadataRef{}, UnsupportedMetadataSourceError{Source: parsed.Source()}
	}
	returned, err := l.metadataSource.GetSeries(ctx, parsed.ID())
	if err != nil {
		return metadata.Series{}, domain.MetadataRef{}, err
	}
	return returned, parsed, nil
}

func (l *Library) metadataSeriesForRecord(ctx context.Context, record SeriesRecord) (metadata.Series, error) {
	ref, err := domain.ParseMetadataRef(record.MetadataRef)
	if err != nil {
		return metadata.Series{}, err
	}
	if ref.Source() != l.metadataSource.Key() {
		return metadata.Series{}, UnsupportedMetadataSourceError{Source: ref.Source()}
	}
	return l.metadataSource.GetSeries(ctx, ref.ID())
}

func (l *Library) checkMetadataRefAvailable(ref domain.MetadataRef, next SeriesRef) error {
	existing, ok, err := l.index.Get(ref)
	if err != nil {
		return err
	}
	if ok && existing.String() != string(next) {
		return MetadataRefConflictError{
			Ref:      MetadataRef(ref.String()),
			Existing: SeriesRef(existing.String()),
			Next:     next,
		}
	}
	return nil
}

func (l *Library) saveIndexRecord(record store.Series, ref SeriesRef) error {
	path, err := domain.ParseSeriesPath(string(ref))
	if err != nil {
		return err
	}
	if err := l.index.Put(record, path); err != nil {
		duplicate, ok := errors.AsType[store.DuplicateLibraryIndexRefError](err)
		if ok {
			return MetadataRefConflictError{
				Ref:      MetadataRef(duplicate.Ref.String()),
				Existing: SeriesRef(duplicate.Existing.String()),
				Next:     SeriesRef(duplicate.Next.String()),
			}
		}
		return err
	}
	return l.index.Save()
}

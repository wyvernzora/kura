package kura

import (
	"context"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/store"
)

func (l *Library) fetchMetadataSeries(ctx context.Context, ref MetadataRef) (metadata.Series, domain.MetadataRef, error) {
	metadataID, err := tvdbID(ref)
	if err != nil {
		return metadata.Series{}, "", err
	}
	returned, err := l.metadataSource.GetSeries(ctx, metadataID)
	if err != nil {
		return metadata.Series{}, "", err
	}
	return returned, domain.MetadataRef(ref), nil
}

func (l *Library) metadataSeriesForRecord(ctx context.Context, record SeriesRecord) (metadata.Series, error) {
	ref := domain.MetadataRef(record.MetadataRef)
	source := ref.Source()
	metadataID := ref.Value()
	if source != l.metadataSource.Key() {
		return metadata.Series{}, UnsupportedMetadataSourceError{Source: source}
	}
	if metadataID == "" {
		return metadata.Series{}, invalidMetadataRefError(ref)
	}
	return l.metadataSource.GetSeries(ctx, metadataID)
}

func (l *Library) checkMetadataRefAvailable(ref domain.MetadataRef, next SeriesRef) error {
	existing, ok, err := l.index.Get(ref)
	if err != nil {
		return err
	}
	if ok && existing.String() != string(next) {
		return MetadataRefConflictError{
			Ref:      MetadataRef(ref),
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
				Ref:      MetadataRef(duplicate.Ref),
				Existing: SeriesRef(duplicate.Existing.String()),
				Next:     SeriesRef(duplicate.Next.String()),
			}
		}
		return err
	}
	return l.index.Save()
}

func tvdbID(ref MetadataRef) (string, error) {
	if ref.Source() != "tvdb" {
		return "", UnsupportedMetadataSourceError{Source: ref.Source()}
	}
	if ref.Value() == "" {
		return "", invalidMetadataRefError(ref)
	}
	return ref.Value(), nil
}

func invalidMetadataRefError(ref domain.MetadataRef) error {
	return fmt.Errorf("invalid metadata ref %q; expected <source>:<id>", ref)
}

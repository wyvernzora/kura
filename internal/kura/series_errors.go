package kura

import (
	"errors"

	seriespkg "github.com/wyvernzora/kura/internal/series"
)

func normalizeSeriesLibraryError(err error) error {
	if notIndexed, ok := errors.AsType[seriespkg.MetadataRefNotIndexedError](err); ok {
		return MetadataRefNotIndexedError{Ref: MetadataRef(notIndexed.Ref)}
	}
	if exists, ok := errors.AsType[seriespkg.SeriesAlreadyExistsError](err); ok {
		return SeriesAlreadyExistsError{Ref: SeriesRef(exists.Ref)}
	}
	if notFound, ok := errors.AsType[seriespkg.SeriesNotFoundError](err); ok {
		return SeriesNotFoundError{Ref: SeriesRef(notFound.Ref)}
	}
	if notTracked, ok := errors.AsType[seriespkg.SeriesNotTrackedError](err); ok {
		return SeriesNotTrackedError{Ref: SeriesRef(notTracked.Ref)}
	}
	if alreadyTracked, ok := errors.AsType[seriespkg.SeriesAlreadyTrackedError](err); ok {
		return SeriesAlreadyTrackedError{Ref: SeriesRef(alreadyTracked.Ref)}
	}
	if conflict, ok := errors.AsType[seriespkg.MetadataRefConflictError](err); ok {
		return MetadataRefConflictError{
			Ref:      MetadataRef(conflict.Ref),
			Existing: SeriesRef(conflict.Existing),
			Next:     SeriesRef(conflict.Next),
		}
	}
	if unsupported, ok := errors.AsType[seriespkg.UnsupportedMetadataSourceError](err); ok {
		return UnsupportedMetadataSourceError{Source: unsupported.Source}
	}
	return err
}

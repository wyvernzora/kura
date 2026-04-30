package kura

import (
	"errors"

	seriespkg "github.com/wyvernzora/kura/internal/series"
)

func normalizeSeriesLibraryError(err error) error {
	var notIndexed seriespkg.MetadataRefNotIndexedError
	if errors.As(err, &notIndexed) {
		return MetadataRefNotIndexedError{Ref: MetadataRef(notIndexed.Ref)}
	}
	var exists seriespkg.SeriesAlreadyExistsError
	if errors.As(err, &exists) {
		return SeriesAlreadyExistsError{Ref: SeriesRef(exists.Ref)}
	}
	var notFound seriespkg.SeriesNotFoundError
	if errors.As(err, &notFound) {
		return SeriesNotFoundError{Ref: SeriesRef(notFound.Ref)}
	}
	var notTracked seriespkg.SeriesNotTrackedError
	if errors.As(err, &notTracked) {
		return SeriesNotTrackedError{Ref: SeriesRef(notTracked.Ref)}
	}
	var alreadyTracked seriespkg.SeriesAlreadyTrackedError
	if errors.As(err, &alreadyTracked) {
		return SeriesAlreadyTrackedError{Ref: SeriesRef(alreadyTracked.Ref)}
	}
	var conflict seriespkg.MetadataRefConflictError
	if errors.As(err, &conflict) {
		return MetadataRefConflictError{
			Ref:      MetadataRef(conflict.Ref),
			Existing: SeriesRef(conflict.Existing),
			Next:     SeriesRef(conflict.Next),
		}
	}
	var unsupported seriespkg.UnsupportedMetadataSourceError
	if errors.As(err, &unsupported) {
		return UnsupportedMetadataSourceError{Source: unsupported.Source}
	}
	return err
}

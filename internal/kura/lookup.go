package kura

import (
	"errors"
	"os"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/store"
)

func (l *Library) Find(ref MetadataRef) (*Series, error) {
	parsed, err := domain.ParseMetadataRef(string(ref))
	if err != nil {
		return nil, err
	}
	path, ok, err := l.index.Get(parsed)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, MetadataRefNotIndexedError{Ref: ref}
	}
	return l.Get(SeriesRef(path.String()))
}

func (l *Library) Get(ref SeriesRef) (*Series, error) {
	seriesDir, err := l.root.SeriesDir(string(ref))
	if errors.Is(err, os.ErrNotExist) {
		return nil, SeriesNotFoundError{Ref: ref}
	}
	if err != nil {
		return nil, err
	}
	record, err := store.LoadSeries(seriesDir.Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, SeriesNotTrackedError{Ref: ref}
	}
	if err != nil {
		return nil, err
	}
	return newSeries(l, ref, *record), nil
}

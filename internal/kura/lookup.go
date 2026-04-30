package kura

import (
	"errors"
	"os"

	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/store"
)

func (l *Library) Find(ref MetadataRef) (*Series, error) {
	if l.series != nil {
		if handle, err := l.series.Find(refs.Metadata(ref)); err == nil {
			model, err := handle.Load()
			if err != nil {
				return nil, err
			}
			return newSeriesModel(l, SeriesRef(handle.Ref()), model), nil
		}
	}
	path, ok, err := l.index.Get(ref)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, MetadataRefNotIndexedError{Ref: ref}
	}
	return l.Get(SeriesRef(path.String()))
}

func (l *Library) Get(ref SeriesRef) (*Series, error) {
	if l.series != nil {
		if handle, err := l.series.Open(refs.Series(ref)); err == nil {
			model, err := handle.Load()
			if err != nil {
				return nil, err
			}
			return newSeriesModel(l, SeriesRef(handle.Ref()), model), nil
		}
	}
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

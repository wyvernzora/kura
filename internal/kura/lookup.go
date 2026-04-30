package kura

import (
	"github.com/wyvernzora/kura/internal/refs"
)

func (l *Library) Find(ref MetadataRef) (*Series, error) {
	handle, err := l.series.Find(refs.Metadata(ref))
	if err != nil {
		return nil, normalizeSeriesLibraryError(err)
	}
	model, err := handle.Load()
	if err != nil {
		return nil, err
	}
	return newSeriesModel(l, SeriesRef(handle.Ref()), model), nil
}

func (l *Library) Get(ref SeriesRef) (*Series, error) {
	handle, err := l.series.Open(refs.Series(ref))
	if err != nil {
		return nil, normalizeSeriesLibraryError(err)
	}
	model, err := handle.Load()
	if err != nil {
		return nil, err
	}
	return newSeriesModel(l, SeriesRef(handle.Ref()), model), nil
}

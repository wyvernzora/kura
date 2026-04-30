package kura

import (
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series"
)

func (l *Library) Find(ref refs.Metadata) (series.Handle, error) {
	handle, err := l.series.Find(ref)
	if err != nil {
		return series.Handle{}, err
	}
	return handle, nil
}

func (l *Library) Get(ref refs.Series) (series.Handle, error) {
	handle, err := l.series.Open(ref)
	if err != nil {
		return series.Handle{}, err
	}
	return handle, nil
}

package domain

// SeriesRef identifies a tracked series by its library-root child directory.
type SeriesRef string

func (ref SeriesRef) String() string {
	return string(ref)
}

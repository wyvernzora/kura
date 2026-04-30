package domain

import "github.com/wyvernzora/kura/internal/refs"

// SeriesPath is a direct child directory name below a library root.
type SeriesPath = refs.Series

func ParseSeriesPath(name string) (SeriesPath, error) {
	return refs.ParseSeries(name)
}

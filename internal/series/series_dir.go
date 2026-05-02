package series

import "github.com/wyvernzora/kura/internal/series/layout"

// SeriesDir is a filesystem directory for one series.
type SeriesDir = layout.SeriesDir

func ParseSeriesDir(path string) (SeriesDir, error) {
	return layout.ParseSeriesDir(path)
}

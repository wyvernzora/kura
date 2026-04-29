package ops

import (
	"fmt"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/store"
)

type InitSeriesOptions struct {
	SeriesDir fsroot.SeriesDir
	Metadata  metadata.Series
}

type InitSeriesResult struct {
	Series     store.Series
	SeriesPath domain.SeriesPath
}

func InitSeries(opts InitSeriesOptions) (InitSeriesResult, error) {
	series, err := store.NewSeries(opts.SeriesDir.Path())
	if err != nil {
		return InitSeriesResult{}, err
	}
	series.MetadataRef = opts.Metadata.MetadataRef
	ref, err := domain.ParseMetadataRef(opts.Metadata.MetadataRef)
	if err != nil {
		return InitSeriesResult{}, err
	}
	if ref.Source() != "tvdb" {
		return InitSeriesResult{}, fmt.Errorf("library: unsupported metadata ref source %q", ref.Source())
	}
	series.PreferredTitle = opts.Metadata.PreferredTitle
	series.CanonicalTitle = opts.Metadata.CanonicalTitle
	seriesPath, err := domain.ParseSeriesPath(opts.SeriesDir.Name())
	if err != nil {
		return InitSeriesResult{}, err
	}
	return InitSeriesResult{Series: *series, SeriesPath: seriesPath}, nil
}

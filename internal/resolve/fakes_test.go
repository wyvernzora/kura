package resolve

import (
	"context"
	"fmt"
	"slices"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/refs"
)

type strategyFakeSource struct {
	key                  string
	searchResults        []metadata.SearchResult
	searchResultsByQuery map[string][]metadata.SearchResult
	searchErr            error
	series               map[string]metadata.Series
	seriesErr            error
}

func (s *strategyFakeSource) Key() string {
	if s.key != "" {
		return s.key
	}
	return "tvdb"
}

func (s *strategyFakeSource) Search(_ context.Context, query string, _ metadata.SearchOptions) ([]metadata.SearchResult, error) {
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	if results, ok := s.searchResultsByQuery[query]; ok {
		return slices.Clone(results), nil
	}
	return slices.Clone(s.searchResults), nil
}

func (s *strategyFakeSource) GetSeries(_ context.Context, metadataID string) (metadata.Series, error) {
	if s.seriesErr != nil {
		return metadata.Series{}, s.seriesErr
	}
	series, ok := s.series[metadataID]
	if !ok {
		return metadata.Series{}, fmt.Errorf("%w: series %s", metadata.ErrNotFound, metadataID)
	}
	return series, nil
}

func testSummary(ref string) metadata.SeriesSummary {
	return metadata.SeriesSummary{
		MetadataRef:    refs.Metadata(ref),
		PreferredTitle: ref + " preferred",
		CanonicalTitle: ref + " canonical",
	}
}

func testMetadataSeries(ref string) metadata.Series {
	return metadata.Series{SeriesSummary: testSummary(ref)}
}

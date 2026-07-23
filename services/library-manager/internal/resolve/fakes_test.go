package resolve

import (
	"context"
	"fmt"
	"slices"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider"
	"github.com/wyvernzora/kura/services/library-manager/internal/textnorm"
)

type strategyFakeSource struct {
	key                  string
	searchResults        []provider.SearchResult
	searchResultsByQuery map[string][]provider.SearchResult
	searchErr            error
	series               map[string]provider.Series
	seriesErr            error
}

func (s *strategyFakeSource) Key() string {
	if s.key != "" {
		return s.key
	}
	return "tvdb"
}

func (s *strategyFakeSource) Search(_ context.Context, query textnorm.NFCString, _ provider.SearchOptions) ([]provider.SearchResult, error) {
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	if results, ok := s.searchResultsByQuery[query.String()]; ok {
		return slices.Clone(results), nil
	}
	return slices.Clone(s.searchResults), nil
}

func (s *strategyFakeSource) GetSeries(_ context.Context, metadataID, _ string) (provider.Series, error) {
	if s.seriesErr != nil {
		return provider.Series{}, s.seriesErr
	}
	series, ok := s.series[metadataID]
	if !ok {
		return provider.Series{}, fmt.Errorf("%w: series %s", provider.ErrNotFound, metadataID)
	}
	return series, nil
}

func testSummary(ref string) provider.SeriesSummary {
	return provider.SeriesSummary{
		MetadataRef:    refs.Metadata(ref),
		PreferredTitle: textnorm.NFC(ref + " preferred"),
		CanonicalTitle: textnorm.NFC(ref + " canonical"),
	}
}

func testMetadataSeries(ref string) provider.Series {
	return provider.Series{SeriesSummary: testSummary(ref)}
}

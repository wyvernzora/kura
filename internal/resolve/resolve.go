package resolve

import (
	"context"
	"fmt"
	"strings"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/metadata"
)

type ResolveSeriesOptions struct {
	ProviderRef string
	SearchLimit int
}

type SeriesSelectionRequiredError struct {
	Query      string
	Candidates []metadata.SearchResult
}

func (err SeriesSelectionRequiredError) Error() string {
	return fmt.Sprintf("no exact metadata match for %q", err.Query)
}

func ResolveProviderSeries(ctx context.Context, metadataSource metadata.Source, dirname string, opts ResolveSeriesOptions) (metadata.Series, bool, error) {
	if opts.ProviderRef != "" {
		series, err := GetProviderSeriesByRef(ctx, metadataSource, opts.ProviderRef)
		return series, true, err
	}

	query := domain.CleanFileTitle(dirname).String()
	results, err := metadataSource.Search(ctx, query, metadata.SearchOptions{
		Limit: opts.SearchLimit,
		Type:  metadata.MediaTypeSeries,
	})
	if err != nil {
		return metadata.Series{}, false, err
	}
	match, ok, err := SearchResultMatch(dirname, results)
	if err != nil {
		return metadata.Series{}, false, err
	}
	if !ok {
		return metadata.Series{}, false, SeriesSelectionRequiredError{Query: dirname, Candidates: results}
	}
	series, err := GetProviderSeriesByRef(ctx, metadataSource, match.ProviderRef)
	return series, false, err
}

func GetProviderSeriesByRef(ctx context.Context, metadataSource metadata.Source, remoteRef string) (metadata.Series, error) {
	ref, err := domain.ParseRemoteSeriesRef(remoteRef)
	if err != nil {
		return metadata.Series{}, err
	}
	if ref.Source() != metadataSource.Key() {
		return metadata.Series{}, fmt.Errorf("unsupported series ref provider %q; expected %s:<id>", ref.Source(), metadataSource.Key())
	}
	return metadataSource.GetSeries(ctx, ref.ID())
}

func SearchResultMatch(dirname string, results []metadata.SearchResult) (metadata.SearchResult, bool, error) {
	match, ok, err := ExactSearchMatch(dirname, results)
	if err != nil || ok {
		return match, ok, err
	}
	if len(results) != 1 {
		return metadata.SearchResult{}, false, nil
	}
	result := results[0]
	if TitleContainsQuery(dirname, result.PreferredTitle) || TitleContainsQuery(dirname, result.CanonicalTitle) {
		return result, true, nil
	}
	return metadata.SearchResult{}, false, nil
}

func ExactSearchMatch(dirname string, results []metadata.SearchResult) (metadata.SearchResult, bool, error) {
	var matches []metadata.SearchResult
	for _, result := range results {
		if ExactTitleMatch(dirname, result.PreferredTitle) || ExactTitleMatch(dirname, result.CanonicalTitle) {
			matches = append(matches, result)
		}
	}
	if len(matches) == 0 {
		return metadata.SearchResult{}, false, nil
	}
	if len(matches) > 1 {
		return metadata.SearchResult{}, false, fmt.Errorf("multiple exact metadata matches for %q", dirname)
	}
	return matches[0], true, nil
}

func ExactTitleMatch(query string, title string) bool {
	return domain.CleanFileTitle(query).String() == domain.CleanFileTitle(title).String()
}

func TitleContainsQuery(query string, title string) bool {
	cleanQuery := domain.CleanFileTitle(query).String()
	if cleanQuery == "" {
		return false
	}
	return strings.Contains(domain.CleanFileTitle(title).String(), cleanQuery)
}

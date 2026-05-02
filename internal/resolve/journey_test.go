package resolve

import (
	"context"
	"errors"
	"testing"

	"github.com/wyvernzora/kura/internal/domain/selector"
	"github.com/wyvernzora/kura/internal/provider"
)

func TestResolverJourneys(t *testing.T) {
	t.Run("clean bootstrap text resolves", func(t *testing.T) {
		source := &strategyFakeSource{
			searchResults: []provider.SearchResult{{SeriesSummary: testSummary("tvdb:370070")}},
		}
		resolver := New(NewTextSearchStrategy(source))
		resolution, err := resolver.Resolve(context.Background(), selector.ParseSelector([]string{"本好きの下剋上"}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(resolution.Results) != 1 {
			t.Fatalf("len(Results) = %d, want 1", len(resolution.Results))
		}
	})

	t.Run("garbled bootstrap unresolved", func(t *testing.T) {
		source := &strategyFakeSource{
			searchResults: []provider.SearchResult{
				{SeriesSummary: testSummary("tvdb:1")},
				{SeriesSummary: testSummary("tvdb:2")},
				{SeriesSummary: testSummary("tvdb:3")},
			},
		}
		resolver := New(NewTextSearchStrategy(source))
		resolution, err := resolver.Resolve(context.Background(), selector.ParseSelector([]string{"Ascendance.of.a.Bookworm.S01.1080p.WEB-DL"}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(resolution.Results) <= 1 {
			t.Fatalf("len(Results) = %d, want >1", len(resolution.Results))
		}
	})

	t.Run("multi-language text terms agree", func(t *testing.T) {
		source := &strategyFakeSource{
			searchResultsByQuery: map[string][]provider.SearchResult{
				"本好きの下剋上": {
					{SeriesSummary: testSummary("tvdb:370070")},
				},
				"Ascendance of a Bookworm": {
					{SeriesSummary: testSummary("tvdb:370070")},
				},
			},
		}
		resolver := New(NewTextSearchStrategy(source))
		resolution, err := resolver.Resolve(context.Background(), selector.ParseSelector([]string{
			"本好きの下剋上",
			"Ascendance of a Bookworm",
		}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(resolution.Results) != 1 {
			t.Fatalf("len(Results) = %d, want 1", len(resolution.Results))
		}
		if got := len(resolution.Results[0].Evidence); got != 2 {
			t.Fatalf("evidence count = %d, want 2", got)
		}
	})

	t.Run("direct id retry resolves", func(t *testing.T) {
		source := &strategyFakeSource{series: map[string]provider.Series{"370070": testMetadataSeries("tvdb:370070")}}
		resolver := New(NewMetadataIDStrategy(source), NewTextSearchStrategy(source))
		resolution, err := resolver.Resolve(context.Background(), selector.ParseSelector([]string{"tvdb:370070"}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(resolution.Results) != 1 {
			t.Fatalf("len(Results) = %d, want 1", len(resolution.Results))
		}
	})

	t.Run("unknown query not found", func(t *testing.T) {
		source := &strategyFakeSource{}
		resolver := New(NewTextSearchStrategy(source))
		resolution, err := resolver.Resolve(context.Background(), selector.ParseSelector([]string{"Season 2"}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(resolution.Results) != 0 {
			t.Fatalf("len(Results) = %d, want 0", len(resolution.Results))
		}
	})

	t.Run("metadata source down errors", func(t *testing.T) {
		source := &strategyFakeSource{searchErr: provider.ErrUnavailable}
		resolver := New(NewTextSearchStrategy(source))
		_, err := resolver.Resolve(context.Background(), selector.ParseSelector([]string{"Bookworm"}))
		if !errors.Is(err, provider.ErrUnavailable) {
			t.Fatalf("error = %v, want ErrUnavailable", err)
		}
	})

	t.Run("dir-prefixed term is text search", func(t *testing.T) {
		source := &strategyFakeSource{
			searchResultsByQuery: map[string][]provider.SearchResult{
				"dir:Bookworm": {
					{SeriesSummary: testSummary("tvdb:370070")},
				},
			},
		}
		resolver := New(NewMetadataIDStrategy(source), NewTextSearchStrategy(source))
		resolution, err := resolver.Resolve(context.Background(), selector.ParseSelector([]string{"dir:Bookworm"}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(resolution.Results) != 1 {
			t.Fatalf("len(Results) = %d, want 1", len(resolution.Results))
		}
	})

	t.Run("unknown-prefixed term is text search", func(t *testing.T) {
		source := &strategyFakeSource{
			searchResultsByQuery: map[string][]provider.SearchResult{
				"foo:Bookworm": {
					{SeriesSummary: testSummary("tvdb:370070")},
				},
			},
		}
		resolver := New(NewMetadataIDStrategy(source), NewTextSearchStrategy(source))
		resolution, err := resolver.Resolve(context.Background(), selector.ParseSelector([]string{"foo:Bookworm"}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(resolution.Results) != 1 {
			t.Fatalf("len(Results) = %d, want 1", len(resolution.Results))
		}
	})

	t.Run("conflicting terms error", func(t *testing.T) {
		source := &strategyFakeSource{}
		resolver := New(NewMetadataIDStrategy(source), NewTextSearchStrategy(source))
		_, err := resolver.Resolve(context.Background(), selector.ParseSelector([]string{"X-Men", "tvdb:370070"}))
		if !errors.Is(err, ErrConflictingTerms) {
			t.Fatalf("error = %v, want ErrConflictingTerms", err)
		}
	})

	t.Run("too many terms error", func(t *testing.T) {
		source := &strategyFakeSource{}
		resolver := New(NewTextSearchStrategy(source))
		raw := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}
		_, err := resolver.Resolve(context.Background(), selector.ParseSelector(raw))
		if !errors.Is(err, ErrTooManyTerms) {
			t.Fatalf("error = %v, want ErrTooManyTerms", err)
		}
	})
}

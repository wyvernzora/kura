package metadata

import (
	"context"
	"testing"
	"time"
)

func TestCachedSourceCachesAndClonesSearchResults(t *testing.T) {
	fake := &fakeSource{
		searchResults: []SearchResult{{
			SeriesSummary: SeriesSummary{
				ProviderRef:    "fake:1",
				ProviderRefs:   []string{"fake:1", "imdb:tt1"},
				PreferredTitle: "Original",
				CanonicalTitle: "Original",
				Genres:         []string{"Fantasy"},
			},
		}},
	}

	cached, err := NewCachedSource(fake, CacheOptions{
		TTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewCachedSource: %v", err)
	}

	first, err := cached.Search(context.Background(), "query", SearchOptions{})
	if err != nil {
		t.Fatalf("first Search: %v", err)
	}
	first[0].PreferredTitle = "Mutated"
	first[0].ProviderRefs[1] = "mutated:1"
	first[0].Genres[0] = "Mutated"

	second, err := cached.Search(context.Background(), "query", SearchOptions{})
	if err != nil {
		t.Fatalf("second Search: %v", err)
	}
	if fake.searchCalls != 1 {
		t.Fatalf("search calls = %d, want 1", fake.searchCalls)
	}
	if got := second[0].PreferredTitle; got != "Original" {
		t.Fatalf("cached title = %q, want Original", got)
	}
	if got := second[0].ProviderRefs[1]; got != "imdb:tt1" {
		t.Fatalf("cached provider ref = %q, want imdb:tt1", got)
	}
	if got := second[0].Genres[0]; got != "Fantasy" {
		t.Fatalf("cached genre = %q, want Fantasy", got)
	}
}

func TestCachedSourceExpires(t *testing.T) {
	fake := &fakeSource{}

	cached, err := NewCachedSource(fake, CacheOptions{
		TTL: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewCachedSource: %v", err)
	}

	if _, err := cached.GetSeries(context.Background(), "1"); err != nil {
		t.Fatalf("first GetSeries: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := cached.GetSeries(context.Background(), "1"); err != nil {
		t.Fatalf("second GetSeries: %v", err)
	}
	if fake.seriesCalls != 2 {
		t.Fatalf("series calls = %d, want 2", fake.seriesCalls)
	}
}

func TestCachedSourceEvictsLeastRecentlyUsed(t *testing.T) {
	fake := &fakeSource{}

	cached, err := NewCachedSource(fake, CacheOptions{
		TTL:        time.Minute,
		MaxEntries: 1,
	})
	if err != nil {
		t.Fatalf("NewCachedSource: %v", err)
	}

	if _, err := cached.GetSeries(context.Background(), "1"); err != nil {
		t.Fatalf("first GetSeries: %v", err)
	}
	if _, err := cached.GetSeries(context.Background(), "2"); err != nil {
		t.Fatalf("second GetSeries: %v", err)
	}
	if _, err := cached.GetSeries(context.Background(), "1"); err != nil {
		t.Fatalf("third GetSeries: %v", err)
	}
	if fake.seriesCalls != 3 {
		t.Fatalf("series calls = %d, want 3", fake.seriesCalls)
	}
}

type fakeSource struct {
	searchCalls   int
	seriesCalls   int
	searchResults []SearchResult
	series        Series
}

func (p *fakeSource) Key() string {
	return "fake"
}

func (p *fakeSource) Search(context.Context, string, SearchOptions) ([]SearchResult, error) {
	p.searchCalls++
	return p.searchResults, nil
}

func (p *fakeSource) GetSeries(context.Context, string) (Series, error) {
	p.seriesCalls++
	return p.series, nil
}

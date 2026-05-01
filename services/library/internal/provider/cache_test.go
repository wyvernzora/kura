package provider

import (
	"context"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/textnorm"
)

func TestCachedSourceSharesSearchResults(t *testing.T) {
	// Cache contract is "callers must not mutate". Subsequent reads
	// share the same underlying slice/maps as the first read so the
	// hot path stays allocation-free. Upstream is hit only once.
	fake := &fakeSource{
		searchResults: []SearchResult{{
			SeriesSummary: SeriesSummary{
				MetadataRef:    "fake:1",
				PreferredTitle: textnorm.NFC("Original"),
				CanonicalTitle: textnorm.NFC("Original"),
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

	first, err := cached.Search(context.Background(), textnorm.NFC("query"), SearchOptions{})
	if err != nil {
		t.Fatalf("first Search: %v", err)
	}
	second, err := cached.Search(context.Background(), textnorm.NFC("query"), SearchOptions{})
	if err != nil {
		t.Fatalf("second Search: %v", err)
	}
	if fake.searchCalls != 1 {
		t.Fatalf("search calls = %d, want 1", fake.searchCalls)
	}
	// Both reads must point at the same backing array (no clone-on-read).
	if &first[0] != &second[0] {
		t.Fatalf("second read returned a different slice header; cache should share storage")
	}
	if got := second[0].PreferredTitle; got.String() != "Original" {
		t.Fatalf("cached title = %q, want Original", got)
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

	if _, err := cached.GetSeries(context.Background(), "1", ""); err != nil {
		t.Fatalf("first GetSeries: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := cached.GetSeries(context.Background(), "1", ""); err != nil {
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

	if _, err := cached.GetSeries(context.Background(), "1", ""); err != nil {
		t.Fatalf("first GetSeries: %v", err)
	}
	if _, err := cached.GetSeries(context.Background(), "2", ""); err != nil {
		t.Fatalf("second GetSeries: %v", err)
	}
	if _, err := cached.GetSeries(context.Background(), "1", ""); err != nil {
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

func (p *fakeSource) Search(context.Context, textnorm.NFCString, SearchOptions) ([]SearchResult, error) {
	p.searchCalls++
	return p.searchResults, nil
}

func (p *fakeSource) GetSeries(context.Context, string, string) (Series, error) {
	p.seriesCalls++
	return p.series, nil
}

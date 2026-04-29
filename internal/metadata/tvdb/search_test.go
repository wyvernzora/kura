package tvdb

import (
	"context"
	"testing"

	"github.com/wyvernzora/kura/internal/metadata"
)

func TestSearchNormalizesSeriesResults(t *testing.T) {
	server := newTestServer(t, nil)
	defer server.Close()

	p, err := New("test-key", Options{
		BaseURL:            server.URL,
		HTTPClient:         server.Client(),
		PreferredLanguages: []string{"ja", "en"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	results, err := p.Search(context.Background(), "honzuki", metadata.SearchOptions{
		Limit: 5,
		Year:  2019,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	got := results[0]
	if got.MetadataRef != "tvdb:370070" {
		t.Fatalf("MetadataRef = %q, want tvdb:370070", got.MetadataRef)
	}
	if got.Score != 42.5 {
		t.Fatalf("Score = %v, want 42.5", got.Score)
	}
	if got.MatchSource != "query" {
		t.Fatalf("MatchSource = %q, want query", got.MatchSource)
	}
	if got.CanonicalTitle != "Ascendance of a Bookworm" {
		t.Fatalf("CanonicalTitle = %q", got.CanonicalTitle)
	}
	if got.PreferredTitle != "本好きの下剋上" {
		t.Fatalf("PreferredTitle = %q, want 本好きの下剋上", got.PreferredTitle)
	}
	if got.Year != 2019 {
		t.Fatalf("Year = %d, want 2019", got.Year)
	}
	if got.Status != metadata.SeriesStatusEnded {
		t.Fatalf("Status = %q, want ended", got.Status)
	}
	if len(got.Genres) != 2 || got.Genres[0] != "Fantasy" || got.Genres[1] != "Anime" {
		t.Fatalf("Genres = %#v, want Fantasy, Anime", got.Genres)
	}
	if got.OriginalLanguage != "ja" {
		t.Fatalf("OriginalLanguage = %q, want ja", got.OriginalLanguage)
	}
	if got.OriginalCountry != "JP" {
		t.Fatalf("OriginalCountry = %q, want JP", got.OriginalCountry)
	}
	if got.FirstAired != "2019-10-03" {
		t.Fatalf("FirstAired = %#v, want 2019-10-03", got.FirstAired)
	}
}

func TestSearchUsesCanonicalTitleWhenNoPreferredLanguages(t *testing.T) {
	server := newTestServer(t, nil)
	defer server.Close()

	p, err := New("test-key", Options{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	results, err := p.Search(context.Background(), "honzuki", metadata.SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].PreferredTitle != "Ascendance of a Bookworm" {
		t.Fatalf("PreferredTitle = %q, want canonical title", results[0].PreferredTitle)
	}
}

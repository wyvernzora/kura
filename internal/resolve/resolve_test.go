package resolve

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/wyvernzora/kura/internal/metadata"
)

func TestResolveProviderSeriesExactMatch(t *testing.T) {
	metadataSource := fakeMetadataSource{
		searchResults: []metadata.SearchResult{{
			SeriesSummary: metadata.SeriesSummary{
				ProviderRef:    "tvdb:370070",
				PreferredTitle: "本好きの下剋上",
				CanonicalTitle: "Ascendance of a Bookworm",
			},
		}},
		series: map[string]metadata.Series{
			"370070": testProviderSeries(),
		},
	}

	series, selected, err := ResolveProviderSeries(context.Background(), metadataSource, "本好きの下剋上", ResolveSeriesOptions{})
	if err != nil {
		t.Fatalf("ResolveProviderSeries: %v", err)
	}
	if selected {
		t.Fatal("selected = true, want false for exact search match")
	}
	if series.ProviderRef != "tvdb:370070" {
		t.Fatalf("ProviderRef = %q, want tvdb:370070", series.ProviderRef)
	}
}

func TestResolveProviderSeriesSingleSubstringMatch(t *testing.T) {
	metadataSource := fakeMetadataSource{
		searchResults: []metadata.SearchResult{{
			SeriesSummary: metadata.SeriesSummary{
				ProviderRef:    "tvdb:370070",
				PreferredTitle: "本好きの下剋上 司書になるためには手段を選んでいられません",
				CanonicalTitle: "Ascendance of a Bookworm",
			},
		}},
		series: map[string]metadata.Series{
			"370070": testProviderSeries(),
		},
	}

	series, selected, err := ResolveProviderSeries(context.Background(), metadataSource, "本好きの下剋上", ResolveSeriesOptions{})
	if err != nil {
		t.Fatalf("ResolveProviderSeries: %v", err)
	}
	if selected {
		t.Fatal("selected = true, want false for search match")
	}
	if series.ProviderRef != "tvdb:370070" {
		t.Fatalf("ProviderRef = %q, want tvdb:370070", series.ProviderRef)
	}
}

func TestResolveProviderSeriesDoesNotSubstringMatchMultipleResults(t *testing.T) {
	metadataSource := fakeMetadataSource{
		searchResults: []metadata.SearchResult{
			{
				SeriesSummary: metadata.SeriesSummary{
					ProviderRef:    "tvdb:1",
					PreferredTitle: "Bookworm Extra",
				},
			},
			{
				SeriesSummary: metadata.SeriesSummary{
					ProviderRef:    "tvdb:2",
					CanonicalTitle: "Bookworm OVA",
				},
			},
		},
	}

	_, _, err := ResolveProviderSeries(context.Background(), metadataSource, "Bookworm", ResolveSeriesOptions{})
	selectionRequired, ok := errors.AsType[SeriesSelectionRequiredError](err)
	if !ok {
		t.Fatalf("error = %v, want SeriesSelectionRequiredError", err)
	}
	if len(selectionRequired.Candidates) != 2 {
		t.Fatalf("len(Candidates) = %d, want 2", len(selectionRequired.Candidates))
	}
}

func TestResolveProviderSeriesReturnsCandidatesWhenSelectionRequired(t *testing.T) {
	metadataSource := fakeMetadataSource{
		searchResults: []metadata.SearchResult{{
			SeriesSummary: metadata.SeriesSummary{
				ProviderRef:    "tvdb:1",
				PreferredTitle: "Candidate",
			},
		}},
	}

	_, _, err := ResolveProviderSeries(context.Background(), metadataSource, "No Match", ResolveSeriesOptions{})
	selectionRequired, ok := errors.AsType[SeriesSelectionRequiredError](err)
	if !ok {
		t.Fatalf("error = %v, want SeriesSelectionRequiredError", err)
	}
	if len(selectionRequired.Candidates) != 1 {
		t.Fatalf("len(Candidates) = %d, want 1", len(selectionRequired.Candidates))
	}
}

type fakeMetadataSource struct {
	searchResults []metadata.SearchResult
	series        map[string]metadata.Series
}

func (p fakeMetadataSource) Key() string {
	return "tvdb"
}

func (p fakeMetadataSource) Search(context.Context, string, metadata.SearchOptions) ([]metadata.SearchResult, error) {
	return slices.Clone(p.searchResults), nil
}

func (p fakeMetadataSource) GetSeries(_ context.Context, providerID string) (metadata.Series, error) {
	series, ok := p.series[providerID]
	if !ok {
		return metadata.Series{}, fmt.Errorf("series %s not found", providerID)
	}
	return series, nil
}

func testProviderSeries() metadata.Series {
	return metadata.Series{
		SeriesSummary: metadata.SeriesSummary{
			ProviderRef:      "tvdb:370070",
			ProviderRefs:     []string{"tvdb:370070", "imdb:tt10885406", "tmdb:12345"},
			PreferredTitle:   "本好きの下剋上",
			CanonicalTitle:   "Ascendance of a Bookworm",
			OriginalLanguage: "jpn",
		},
	}
}

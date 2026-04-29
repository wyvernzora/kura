package resolve

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/wyvernzora/kura/internal/metadata"
)

func TestResolveMetadataSeriesExactMatch(t *testing.T) {
	metadataSource := fakeMetadataSource{
		searchResults: []metadata.SearchResult{{
			SeriesSummary: metadata.SeriesSummary{
				MetadataRef:    "tvdb:370070",
				PreferredTitle: "本好きの下剋上",
				CanonicalTitle: "Ascendance of a Bookworm",
			},
		}},
		series: map[string]metadata.Series{
			"370070": testResolvedMetadataSeries(),
		},
	}

	series, selected, err := ResolveMetadataSeries(context.Background(), metadataSource, "本好きの下剋上", ResolveSeriesOptions{})
	if err != nil {
		t.Fatalf("ResolveMetadataSeries: %v", err)
	}
	if selected {
		t.Fatal("selected = true, want false for exact search match")
	}
	if series.MetadataRef != "tvdb:370070" {
		t.Fatalf("MetadataRef = %q, want tvdb:370070", series.MetadataRef)
	}
}

func TestResolveMetadataSeriesSingleSubstringMatch(t *testing.T) {
	metadataSource := fakeMetadataSource{
		searchResults: []metadata.SearchResult{{
			SeriesSummary: metadata.SeriesSummary{
				MetadataRef:    "tvdb:370070",
				PreferredTitle: "本好きの下剋上 司書になるためには手段を選んでいられません",
				CanonicalTitle: "Ascendance of a Bookworm",
			},
		}},
		series: map[string]metadata.Series{
			"370070": testResolvedMetadataSeries(),
		},
	}

	series, selected, err := ResolveMetadataSeries(context.Background(), metadataSource, "本好きの下剋上", ResolveSeriesOptions{})
	if err != nil {
		t.Fatalf("ResolveMetadataSeries: %v", err)
	}
	if selected {
		t.Fatal("selected = true, want false for search match")
	}
	if series.MetadataRef != "tvdb:370070" {
		t.Fatalf("MetadataRef = %q, want tvdb:370070", series.MetadataRef)
	}
}

func TestResolveMetadataSeriesDoesNotSubstringMatchMultipleResults(t *testing.T) {
	metadataSource := fakeMetadataSource{
		searchResults: []metadata.SearchResult{
			{
				SeriesSummary: metadata.SeriesSummary{
					MetadataRef:    "tvdb:1",
					PreferredTitle: "Bookworm Extra",
				},
			},
			{
				SeriesSummary: metadata.SeriesSummary{
					MetadataRef:    "tvdb:2",
					CanonicalTitle: "Bookworm OVA",
				},
			},
		},
	}

	_, _, err := ResolveMetadataSeries(context.Background(), metadataSource, "Bookworm", ResolveSeriesOptions{})
	selectionRequired, ok := errors.AsType[SeriesSelectionRequiredError](err)
	if !ok {
		t.Fatalf("error = %v, want SeriesSelectionRequiredError", err)
	}
	if len(selectionRequired.Candidates) != 2 {
		t.Fatalf("len(Candidates) = %d, want 2", len(selectionRequired.Candidates))
	}
}

func TestResolveMetadataSeriesReturnsCandidatesWhenSelectionRequired(t *testing.T) {
	metadataSource := fakeMetadataSource{
		searchResults: []metadata.SearchResult{{
			SeriesSummary: metadata.SeriesSummary{
				MetadataRef:    "tvdb:1",
				PreferredTitle: "Candidate",
			},
		}},
	}

	_, _, err := ResolveMetadataSeries(context.Background(), metadataSource, "No Match", ResolveSeriesOptions{})
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

func (p fakeMetadataSource) GetSeries(_ context.Context, metadataID string) (metadata.Series, error) {
	series, ok := p.series[metadataID]
	if !ok {
		return metadata.Series{}, fmt.Errorf("series %s not found", metadataID)
	}
	return series, nil
}

func testResolvedMetadataSeries() metadata.Series {
	return metadata.Series{
		SeriesSummary: metadata.SeriesSummary{
			MetadataRef:      "tvdb:370070",
			PreferredTitle:   "本好きの下剋上",
			CanonicalTitle:   "Ascendance of a Bookworm",
			OriginalLanguage: "jpn",
		},
	}
}

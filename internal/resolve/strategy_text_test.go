package resolve

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/textnorm"
)

func TestTextSearchStrategyResolveEmpty(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{})
	hits, err := strategy.Resolve(context.Background(), Term{Value: n("missing")})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("len(hits) = %d, want 0", len(hits))
	}
}

func TestTextSearchStrategyResolveOne(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{
		searchResults: []metadata.SearchResult{{
			SeriesSummary: testSummary("tvdb:1"),
			MatchSource:   "title",
		}},
	})
	hits, err := strategy.Resolve(context.Background(), Term{Value: n("query")})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Rank != 0 || hits[0].MetadataRef != "tvdb:1" {
		t.Fatalf("hit = %#v, want rank 0 tvdb:1", hits[0])
	}
	if hits[0].MatchSource != "title" {
		t.Fatalf("MatchSource = %q, want title", hits[0].MatchSource)
	}
}

func TestTextSearchStrategyAddsMatchAnnotations(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{
		searchResults: []metadata.SearchResult{{
			SeriesSummary: metadata.SeriesSummary{
				MetadataRef:    "tvdb:1",
				PreferredTitle: textnorm.NFC("Ascendance of a Bookworm"),
				CanonicalTitle: textnorm.NFC("本好きの下剋上"),
			},
			Aliases: []textnorm.NFCString{
				textnorm.NFC("Ascendance of a Bookworm"),
				textnorm.NFC("本好きの下剋上"),
				textnorm.NFC("Honzuki no Gekokujou"),
			},
		}},
	})

	full, err := strategy.Resolve(context.Background(), Term{Value: n("本好きの下剋上")})
	if err != nil {
		t.Fatalf("Resolve full: %v", err)
	}
	if len(full) != 1 || !slices.Equal(full[0].Annotations, []string{"full_match"}) {
		t.Fatalf("full annotations = %#v, want full_match", full)
	}

	partial, err := strategy.Resolve(context.Background(), Term{Value: n("bookworm")})
	if err != nil {
		t.Fatalf("Resolve partial: %v", err)
	}
	if len(partial) != 1 || !slices.Equal(partial[0].Annotations, []string{"partial_match"}) {
		t.Fatalf("partial annotations = %#v, want partial_match", partial)
	}
}

func TestTextSearchStrategyResolveRanks(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{
		searchResults: []metadata.SearchResult{
			{SeriesSummary: testSummary("tvdb:1")},
			{SeriesSummary: testSummary("tvdb:2")},
			{SeriesSummary: testSummary("tvdb:3")},
		},
	})
	hits, err := strategy.Resolve(context.Background(), Term{Value: n("query")})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for i, hit := range hits {
		if hit.Rank != i {
			t.Fatalf("hit[%d].Rank = %d, want %d", i, hit.Rank, i)
		}
	}
}

func TestTextSearchStrategyPropagatesError(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{searchErr: metadata.ErrUnauthorized})
	_, err := strategy.Resolve(context.Background(), Term{Value: n("query")})
	if !errors.Is(err, metadata.ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
}

func TestTextSearchStrategyNotFound(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{searchErr: metadata.ErrNotFound})
	hits, err := strategy.Resolve(context.Background(), Term{Value: n("query")})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("len(hits) = %d, want 0", len(hits))
	}
}

func TestTextSearchStrategyProperties(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{})
	matched, stop := strategy.Match(Term{Value: n("query")})
	if !matched {
		t.Fatal("Match text = false, want true")
	}
	if stop {
		t.Fatal("Match text stop = true, want false")
	}
	matched, stop = strategy.Match(Term{Prefix: "unknown", Value: n("Bookworm")})
	if !matched {
		t.Fatal("Match prefixed = false, want true")
	}
	if stop {
		t.Fatal("Match prefixed stop = true, want false")
	}
	if strategy.Authoritative() {
		t.Fatal("Authoritative = true, want false")
	}
}

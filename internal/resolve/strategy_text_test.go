package resolve

import (
	"context"
	"errors"
	"testing"

	"github.com/wyvernzora/kura/internal/metadata"
)

func TestTextSearchStrategyResolveEmpty(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{})
	hits, err := strategy.Resolve(context.Background(), Term{Value: "missing"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("len(hits) = %d, want 0", len(hits))
	}
}

func TestTextSearchStrategyResolveOne(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{
		searchResults: []metadata.SearchResult{{SeriesSummary: testSummary("tvdb:1")}},
	})
	hits, err := strategy.Resolve(context.Background(), Term{Value: "query"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Rank != 0 || hits[0].MetadataRef != "tvdb:1" {
		t.Fatalf("hit = %#v, want rank 0 tvdb:1", hits[0])
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
	hits, err := strategy.Resolve(context.Background(), Term{Value: "query"})
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
	_, err := strategy.Resolve(context.Background(), Term{Value: "query"})
	if !errors.Is(err, metadata.ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
}

func TestTextSearchStrategyNotFound(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{searchErr: metadata.ErrNotFound})
	hits, err := strategy.Resolve(context.Background(), Term{Value: "query"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("len(hits) = %d, want 0", len(hits))
	}
}

func TestTextSearchStrategyProperties(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{})
	if !strategy.Match(Term{Value: "query"}) {
		t.Fatal("Match text = false, want true")
	}
	if strategy.Match(Term{Prefix: "tvdb", Value: "1"}) {
		t.Fatal("Match prefixed = true, want false")
	}
	if strategy.Authoritative() {
		t.Fatal("Authoritative = true, want false")
	}
}

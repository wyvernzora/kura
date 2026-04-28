package resolve

import (
	"context"
	"errors"
	"testing"

	"github.com/wyvernzora/kura/internal/metadata"
)

func TestProviderIDStrategyProperties(t *testing.T) {
	strategy := NewProviderIDStrategy(&strategyFakeSource{key: "tvdb"})
	if !strategy.Match(Term{Prefix: "tvdb", Value: "1"}) {
		t.Fatal("Match tvdb = false, want true")
	}
	if strategy.Match(Term{Prefix: "tmdb", Value: "1"}) {
		t.Fatal("Match tmdb = true, want false")
	}
	if !strategy.Authoritative() {
		t.Fatal("Authoritative = false, want true")
	}
}

func TestProviderIDStrategyNotFound(t *testing.T) {
	strategy := NewProviderIDStrategy(&strategyFakeSource{seriesErr: metadata.ErrNotFound})
	hits, err := strategy.Resolve(context.Background(), Term{Prefix: "tvdb", Value: "1"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("len(hits) = %d, want 0", len(hits))
	}
}

func TestProviderIDStrategyPropagatesError(t *testing.T) {
	strategy := NewProviderIDStrategy(&strategyFakeSource{seriesErr: metadata.ErrUnavailable})
	_, err := strategy.Resolve(context.Background(), Term{Prefix: "tvdb", Value: "1"})
	if !errors.Is(err, metadata.ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestProviderIDStrategyResolveSeries(t *testing.T) {
	strategy := NewProviderIDStrategy(&strategyFakeSource{
		series: map[string]metadata.Series{"1": testMetadataSeries("tvdb:1")},
	})
	hits, err := strategy.Resolve(context.Background(), Term{Prefix: "tvdb", Value: "1"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Rank != 0 || hits[0].ProviderRef != "tvdb:1" || hits[0].Strategy != "provider_id" {
		t.Fatalf("hit = %#v, want rank 0 tvdb:1 provider_id", hits[0])
	}
}

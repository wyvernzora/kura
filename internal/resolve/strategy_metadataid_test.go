package resolve

import (
	"context"
	"errors"
	"testing"

	"github.com/wyvernzora/kura/internal/metadata"
)

func TestMetadataIDStrategyProperties(t *testing.T) {
	strategy := NewMetadataIDStrategy(&strategyFakeSource{key: "tvdb"})
	matched, stop := strategy.Match(Term("tvdb:1"))
	if !matched {
		t.Fatal("Match tvdb = false, want true")
	}
	if !stop {
		t.Fatal("Match tvdb stop = false, want true")
	}
	matched, stop = strategy.Match(Term("tmdb:1"))
	if matched {
		t.Fatal("Match tmdb = true, want false")
	}
	if stop {
		t.Fatal("Match tmdb stop = true, want false")
	}
	if !strategy.Authoritative() {
		t.Fatal("Authoritative = false, want true")
	}
}

func TestMetadataIDStrategyNotFound(t *testing.T) {
	strategy := NewMetadataIDStrategy(&strategyFakeSource{seriesErr: metadata.ErrNotFound})
	hits, err := strategy.Resolve(context.Background(), Term("tvdb:1"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("len(hits) = %d, want 0", len(hits))
	}
}

func TestMetadataIDStrategyPropagatesError(t *testing.T) {
	strategy := NewMetadataIDStrategy(&strategyFakeSource{seriesErr: metadata.ErrUnavailable})
	_, err := strategy.Resolve(context.Background(), Term("tvdb:1"))
	if !errors.Is(err, metadata.ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestMetadataIDStrategyResolveSeries(t *testing.T) {
	strategy := NewMetadataIDStrategy(&strategyFakeSource{
		series: map[string]metadata.Series{"1": testMetadataSeries("tvdb:1")},
	})
	hits, err := strategy.Resolve(context.Background(), Term("tvdb:1"))
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

package resolve

import (
	"context"
	"errors"

	"github.com/wyvernzora/kura/internal/metadata"
)

type textSearchStrategy struct {
	source metadata.Source
}

func NewTextSearchStrategy(source metadata.Source) ResolveStrategy {
	return &textSearchStrategy{source: source}
}

func (s *textSearchStrategy) Name() string {
	return "text_search"
}

func (s *textSearchStrategy) Match(t Term) bool {
	return t.Prefix == ""
}

func (s *textSearchStrategy) Authoritative() bool {
	return false
}

func (s *textSearchStrategy) Resolve(ctx context.Context, t Term) ([]termHit, error) {
	results, err := s.source.Search(ctx, t.Value, metadata.SearchOptions{Type: metadata.MediaTypeSeries})
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	hits := make([]termHit, 0, len(results))
	for i, result := range results {
		hits = append(hits, termHit{
			Term:        t,
			ProviderRef: result.ProviderRef,
			Summary:     result.SeriesSummary,
			Rank:        i,
		})
	}
	return hits, nil
}

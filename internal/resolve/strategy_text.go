package resolve

import (
	"context"
	"errors"
	"strings"

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
	return true
}

func (s *textSearchStrategy) Authoritative() bool {
	return false
}

func (s *textSearchStrategy) Resolve(ctx context.Context, t Term) ([]termHit, error) {
	query := t.String()
	results, err := s.source.Search(ctx, query, metadata.SearchOptions{Type: metadata.MediaTypeSeries})
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
			MetadataRef: result.MetadataRef,
			Summary:     result.SeriesSummary,
			Rank:        i,
			MatchSource: result.MatchSource,
			Annotations: matchAnnotations(query, result),
		})
	}
	return hits, nil
}

func matchAnnotations(term string, result metadata.SearchResult) []string {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return nil
	}
	for _, title := range matchTitles(result) {
		title = strings.ToLower(strings.TrimSpace(title))
		if title == "" {
			continue
		}
		if term == title {
			return []string{"full_match"}
		}
	}
	for _, title := range matchTitles(result) {
		title = strings.ToLower(strings.TrimSpace(title))
		if title != "" && strings.Contains(title, term) {
			return []string{"partial_match"}
		}
	}
	return nil
}

func matchTitles(result metadata.SearchResult) []string {
	return result.Aliases
}

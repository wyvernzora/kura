package resolve

import (
	"context"
	"errors"

	"github.com/wyvernzora/kura/internal/metadata"
)

type providerIDStrategy struct {
	source metadata.Source
}

func NewProviderIDStrategy(source metadata.Source) ResolveStrategy {
	return &providerIDStrategy{source: source}
}

func (s *providerIDStrategy) Name() string {
	return "provider_id"
}

func (s *providerIDStrategy) Match(t Term) bool {
	return t.Prefix == s.source.Key()
}

func (s *providerIDStrategy) Authoritative() bool {
	return true
}

func (s *providerIDStrategy) Resolve(ctx context.Context, t Term) ([]termHit, error) {
	series, err := s.source.GetSeries(ctx, t.Value)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return []termHit{{
		Term:        t,
		ProviderRef: series.ProviderRef,
		Summary:     series.SeriesSummary,
		Rank:        0,
	}}, nil
}

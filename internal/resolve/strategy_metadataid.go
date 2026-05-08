package resolve

import (
	"context"
	"errors"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/selector"
	"github.com/wyvernzora/kura/internal/provider"
)

type metadataIDStrategy struct {
	source provider.Source
}

func NewMetadataIDStrategy(source provider.Source) ResolveStrategy {
	return &metadataIDStrategy{source: source}
}

func (s *metadataIDStrategy) Name() string {
	return "metadata_id"
}

func (s *metadataIDStrategy) Match(t selector.Term) (matched bool, stop bool) {
	ref, err := refs.ParseMetadata(t.String())
	if err == nil && ref.Provider() == s.source.Key() {
		return true, true
	}
	return false, false
}

func (s *metadataIDStrategy) Authoritative() bool {
	return true
}

func (s *metadataIDStrategy) Resolve(ctx context.Context, t selector.Term) ([]termHit, error) {
	ref, err := refs.ParseMetadata(t.String())
	if err != nil {
		return nil, err
	}
	series, err := s.source.GetSeries(ctx, ref.ID(), "")
	if err != nil {
		if errors.Is(err, provider.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return []termHit{{
		Term:        t,
		MetadataRef: series.MetadataRef,
		Summary:     series.SeriesSummary,
		Rank:        0,
	}}, nil
}

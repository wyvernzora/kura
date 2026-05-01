package resolve

import (
	"context"
	"errors"

	"github.com/wyvernzora/kura/internal/metadata"
)

type metadataIDStrategy struct {
	source metadata.Source
}

func NewMetadataIDStrategy(source metadata.Source) ResolveStrategy {
	return &metadataIDStrategy{source: source}
}

func (s *metadataIDStrategy) Name() string {
	return "metadata_id"
}

func (s *metadataIDStrategy) Match(t Term) bool {
	return t.Prefix == s.source.Key()
}

func (s *metadataIDStrategy) Authoritative() bool {
	return true
}

func (s *metadataIDStrategy) Resolve(ctx context.Context, t Term) ([]termHit, error) {
	series, err := s.source.GetSeries(ctx, t.Value.String())
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
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

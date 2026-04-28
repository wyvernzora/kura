package resolve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/store"
)

type dirnameStrategy struct {
	root   fsroot.LibraryRoot
	source metadata.Source
}

func NewDirnameStrategy(root fsroot.LibraryRoot, source metadata.Source) ResolveStrategy {
	return &dirnameStrategy{root: root, source: source}
}

func (s *dirnameStrategy) Name() string {
	return "dirname"
}

func (s *dirnameStrategy) Match(t Term) bool {
	return t.Prefix == "dir"
}

func (s *dirnameStrategy) Authoritative() bool {
	return true
}

func (s *dirnameStrategy) Resolve(ctx context.Context, t Term) ([]termHit, error) {
	dir, err := s.root.SeriesDir(t.Value)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	series, err := store.LoadSeries(dir.Path())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrCorruptSeriesFile, dir.Path(), err)
	}
	refValue, err := preferredProviderRef(*series)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrCorruptSeriesFile, dir.Path(), err)
	}
	ref, err := domain.ParseRemoteSeriesRef(refValue)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrCorruptSeriesFile, dir.Path(), err)
	}
	if ref.Source() != s.source.Key() {
		return nil, fmt.Errorf("%w: %s: provider %q does not match configured provider %q", ErrCorruptSeriesFile, dir.Path(), ref.Source(), s.source.Key())
	}

	providerSeries, err := s.source.GetSeries(ctx, ref.ID())
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrStaleProviderRef, ref)
		}
		return nil, err
	}
	return []termHit{{
		Term:        t,
		ProviderRef: providerSeries.ProviderRef,
		Summary:     providerSeries.SeriesSummary,
		Rank:        0,
	}}, nil
}

func preferredProviderRef(series store.Series) (string, error) {
	if len(series.ProviderRefs) == 0 {
		return "", errors.New("no provider refs")
	}
	for _, ref := range series.ProviderRefs {
		parsed, err := domain.ParseRemoteSeriesRef(ref)
		if err != nil {
			continue
		}
		if series.PreferredProvider != "" && parsed.Source() == series.PreferredProvider {
			return ref, nil
		}
	}
	return series.ProviderRefs[0], nil
}

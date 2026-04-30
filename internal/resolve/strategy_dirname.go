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
	ref := domain.MetadataRef(series.MetadataRef)
	if ref.Source() == "" || ref.Value() == "" {
		return nil, fmt.Errorf("%w: %s: metadata ref is required", ErrCorruptSeriesFile, dir.Path())
	}
	if ref.Source() != s.source.Key() {
		return nil, fmt.Errorf("%w: %s: metadata ref source %q does not match configured source %q", ErrCorruptSeriesFile, dir.Path(), ref.Source(), s.source.Key())
	}

	metadataSeries, err := s.source.GetSeries(ctx, ref.Value())
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrStaleMetadataRef, series.MetadataRef)
		}
		return nil, err
	}
	return []termHit{{
		Term:        t,
		MetadataRef: metadataSeries.MetadataRef,
		Summary:     metadataSeries.SeriesSummary,
		Rank:        0,
	}}, nil
}

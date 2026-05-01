package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series"
	"github.com/wyvernzora/kura/internal/series/wire"
)

type Library struct {
	root    Root
	source  metadata.Source
	inspect mediainfo.Inspector
	index   *Index
	now     func() time.Time
}

type AddInput struct {
	Metadata refs.Metadata
	Ref      refs.Series
}

type ImportInput struct {
	Metadata refs.Metadata
	Ref      refs.Series
	Force    bool
}

func New(root Root, source metadata.Source, inspector mediainfo.Inspector, idx *Index) *Library {
	return &Library{
		root:    root,
		source:  source,
		inspect: inspector,
		index:   idx,
		now:     time.Now,
	}
}

func (l *Library) LibraryRoot() string {
	return l.root.Path()
}

func (l *Library) MetadataSource() metadata.Source {
	return l.source
}

func (l *Library) MediaInspector() series.Inspector {
	return l.inspect
}

func (l *Library) Now() time.Time {
	return l.now()
}

func (l *Library) Add(ctx context.Context, in AddInput) (series.Handle, error) {
	metadataSeries, metadataRef, err := l.fetchMetadata(ctx, in.Metadata)
	if err != nil {
		return series.Handle{}, err
	}
	ref := in.Ref
	if ref.IsZero() {
		title, err := series.ParseFileTitle(metadataSeries.PreferredTitle.String())
		if err != nil {
			return series.Handle{}, err
		}
		ref, err = refs.ParseSeries(title.String())
		if err != nil {
			return series.Handle{}, err
		}
	}
	if _, err := refs.ParseSeries(ref.String()); err != nil {
		return series.Handle{}, err
	}
	target := l.root.Join(ref.String())
	if _, err := os.Stat(target); err == nil {
		return series.Handle{}, series.SeriesAlreadyExistsError{Ref: ref}
	} else if !errors.Is(err, os.ErrNotExist) {
		return series.Handle{}, err
	}
	if err := l.checkMetadataAvailable(metadataRef, ref); err != nil {
		return series.Handle{}, err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return series.Handle{}, err
	}
	if err := series.Initialize(l.root.Path(), ref, metadataRef, metadataSeries); err != nil {
		return series.Handle{}, err
	}
	if err := l.index.Put(metadataRef, ref); err != nil {
		return series.Handle{}, err
	}
	if err := l.index.Save(); err != nil {
		return series.Handle{}, err
	}
	return series.NewHandle(l, ref)
}

func (l *Library) Import(ctx context.Context, in ImportInput) (series.Handle, error) {
	if in.Ref.IsZero() {
		return series.Handle{}, errors.New("series: series ref is required")
	}
	ref, err := refs.ParseSeries(in.Ref.String())
	if err != nil {
		return series.Handle{}, err
	}
	metadataSeries, metadataRef, err := l.fetchMetadata(ctx, in.Metadata)
	if err != nil {
		return series.Handle{}, err
	}
	seriesDir, err := series.ParseSeriesDir(l.root.Join(ref.String()))
	if errors.Is(err, os.ErrNotExist) {
		return series.Handle{}, series.SeriesNotFoundError{Ref: ref}
	}
	if err != nil {
		return series.Handle{}, err
	}
	metadataPath := wire.SeriesMetadataPath(seriesDir.Path())
	if _, err := os.Stat(metadataPath); err == nil {
		if !in.Force {
			return series.Handle{}, series.SeriesAlreadyTrackedError{Ref: ref}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return series.Handle{}, err
	}
	if err := l.checkMetadataAvailable(metadataRef, ref); err != nil {
		return series.Handle{}, err
	}
	if in.Force {
		l.index.Remove(ref)
		if err := os.Remove(metadataPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return series.Handle{}, err
		}
	}
	if err := series.Initialize(l.root.Path(), ref, metadataRef, metadataSeries); err != nil {
		return series.Handle{}, err
	}
	if err := l.index.Put(metadataRef, ref); err != nil {
		return series.Handle{}, err
	}
	if err := l.index.Save(); err != nil {
		return series.Handle{}, err
	}
	return series.NewHandle(l, ref)
}

func (l *Library) Open(ref refs.Series) (series.Handle, error) {
	return series.NewHandle(l, ref)
}

func (l *Library) Find(ref refs.Metadata) (series.Handle, error) {
	seriesRef, ok, err := l.index.Get(ref)
	if err != nil {
		return series.Handle{}, err
	}
	if !ok {
		return series.Handle{}, series.MetadataRefNotIndexedError{Ref: ref}
	}
	return l.Open(seriesRef)
}

func (l *Library) fetchMetadata(ctx context.Context, ref refs.Metadata) (metadata.Series, refs.Metadata, error) {
	if ref.Provider() == "" || ref.ID() == "" {
		return metadata.Series{}, "", fmt.Errorf("invalid metadata ref %q; expected <provider>:<id>", ref)
	}
	if ref.Provider() != l.source.Key() {
		return metadata.Series{}, "", series.UnsupportedMetadataSourceError{Source: ref.Provider()}
	}
	series, err := l.source.GetSeries(ctx, ref.ID())
	if err != nil {
		return metadata.Series{}, "", err
	}
	return series, ref, nil
}

func (l *Library) checkMetadataAvailable(metadataRef refs.Metadata, next refs.Series) error {
	existing, ok, err := l.index.Get(metadataRef)
	if err != nil {
		return err
	}
	if ok && existing != next {
		return series.MetadataRefConflictError{Ref: metadataRef, Existing: existing, Next: next}
	}
	return nil
}

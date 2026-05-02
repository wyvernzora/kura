package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/series"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

type Library struct {
	root    Root
	source  metadata.Source
	inspect media.Inspector
	index   *indexfile.Index
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

func New(root Root, source metadata.Source, inspector media.Inspector, idx *indexfile.Index) *Library {
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

func (l *Library) MediaInspector() media.Inspector {
	return l.inspect
}

func (l *Library) Now() time.Time {
	return l.now()
}

func (l *Library) Add(ctx context.Context, in AddInput) (series.Handle, error) {
	progress.Start(ctx, "add", "Fetching series metadata", 0)
	metadataSeries, metadataRef, err := l.fetchMetadata(ctx, in.Metadata)
	if err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 0, 0)
		return series.Handle{}, err
	}
	ref := in.Ref
	if ref.IsZero() {
		title, err := series.ParseFileTitle(metadataSeries.PreferredTitle.String())
		if err != nil {
			progress.Failure(ctx, "add", "Failed to add series", 0, 0)
			return series.Handle{}, err
		}
		ref, err = refs.ParseSeries(title.String())
		if err != nil {
			progress.Failure(ctx, "add", "Failed to add series", 0, 0)
			return series.Handle{}, err
		}
	}
	if _, err := refs.ParseSeries(ref.String()); err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 0, 0)
		return series.Handle{}, err
	}
	target := l.root.Join(ref.String())
	if _, err := os.Stat(target); err == nil {
		progress.Failure(ctx, "add", "Failed to add series", 0, 0)
		return series.Handle{}, series.SeriesAlreadyExistsError{Ref: ref}
	} else if !errors.Is(err, os.ErrNotExist) {
		progress.Failure(ctx, "add", "Failed to add series", 0, 0)
		return series.Handle{}, err
	}
	if err := l.checkMetadataAvailable(metadataRef, ref); err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 0, 0)
		return series.Handle{}, err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 0, 0)
		return series.Handle{}, err
	}
	progress.Update(ctx, "add", fmt.Sprintf("Writing metadata for %s", ref), 1, 0)
	if err := series.Initialize(l.root.Path(), ref, metadataRef, metadataSeries); err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 1, 0)
		return series.Handle{}, err
	}
	if err := l.index.Put(metadataRef, ref); err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 1, 0)
		return series.Handle{}, err
	}
	if err := l.index.Save(); err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 1, 0)
		return series.Handle{}, err
	}
	progress.Success(ctx, "add", fmt.Sprintf("Added %s", ref), 1)
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
	progress.Start(ctx, "import", fmt.Sprintf("Fetching metadata for %s", ref), 0)
	metadataSeries, metadataRef, err := l.fetchMetadata(ctx, in.Metadata)
	if err != nil {
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 0, 0)
		return series.Handle{}, err
	}
	if _, err := series.ParseSeriesDir(paths.SeriesDir(l.root.Path(), ref)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 0, 0)
			return series.Handle{}, series.SeriesNotFoundError{Ref: ref}
		}
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 0, 0)
		return series.Handle{}, err
	}
	metadataPath := paths.SeriesMetadata(l.root.Path(), ref)
	if _, err := os.Stat(metadataPath); err == nil {
		if !in.Force {
			progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 0, 0)
			return series.Handle{}, series.SeriesAlreadyTrackedError{Ref: ref}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 0, 0)
		return series.Handle{}, err
	}
	if err := l.checkMetadataAvailable(metadataRef, ref); err != nil {
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 0, 0)
		return series.Handle{}, err
	}
	if in.Force {
		l.index.Remove(ref)
		if err := os.Remove(metadataPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 0, 0)
			return series.Handle{}, err
		}
	}
	progress.Update(ctx, "import", fmt.Sprintf("Writing metadata for %s", ref), 1, 0)
	if err := series.Initialize(l.root.Path(), ref, metadataRef, metadataSeries); err != nil {
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 1, 0)
		return series.Handle{}, err
	}
	if err := l.index.Put(metadataRef, ref); err != nil {
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 1, 0)
		return series.Handle{}, err
	}
	if err := l.index.Save(); err != nil {
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 1, 0)
		return series.Handle{}, err
	}
	progress.Success(ctx, "import", fmt.Sprintf("Imported %s", ref), 1)
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

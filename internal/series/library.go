package series

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/index"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/refs"
)

type Library struct {
	root    fsroot.LibraryRoot
	source  metadata.Source
	inspect mediainfo.Inspector
	index   *index.Index
	repo    repo
	files   files
	now     func() time.Time
}

type AddInput struct {
	Metadata refs.Metadata
	Ref      refs.Series
}

type ImportInput struct {
	Metadata refs.Metadata
	Ref      refs.Series
}

func NewLibrary(root fsroot.LibraryRoot, source metadata.Source, inspector mediainfo.Inspector, idx *index.Index) *Library {
	return &Library{
		root:    root,
		source:  source,
		inspect: inspector,
		index:   idx,
		repo:    repo{root: root},
		files:   files{root: root},
		now:     time.Now,
	}
}

func (l *Library) Add(ctx context.Context, in AddInput) (Handle, error) {
	metadataSeries, metadataRef, err := l.fetchMetadata(ctx, in.Metadata)
	if err != nil {
		return Handle{}, err
	}
	ref := in.Ref
	if ref.IsZero() {
		title, err := domain.ParseFileTitle(metadataSeries.PreferredTitle)
		if err != nil {
			return Handle{}, err
		}
		ref, err = refs.ParseSeries(title.String())
		if err != nil {
			return Handle{}, err
		}
	}
	if _, err := refs.ParseSeries(ref.String()); err != nil {
		return Handle{}, err
	}
	target := l.root.Join(ref.String())
	if _, err := os.Stat(target); err == nil {
		return Handle{}, SeriesAlreadyExistsError{Ref: ref}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Handle{}, err
	}
	if err := l.checkMetadataAvailable(metadataRef, ref); err != nil {
		return Handle{}, err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return Handle{}, err
	}
	series, err := buildSeries(metadataRef, metadataSeries, l.now())
	if err != nil {
		return Handle{}, err
	}
	if err := l.repo.save(ref, series); err != nil {
		return Handle{}, err
	}
	if err := l.index.Put(metadataRef, ref); err != nil {
		return Handle{}, err
	}
	if err := l.index.Save(); err != nil {
		return Handle{}, err
	}
	return Handle{lib: l, ref: ref}, nil
}

func (l *Library) Import(ctx context.Context, in ImportInput) (Handle, error) {
	if in.Ref.IsZero() {
		return Handle{}, errors.New("series: series ref is required")
	}
	ref, err := refs.ParseSeries(in.Ref.String())
	if err != nil {
		return Handle{}, err
	}
	metadataSeries, metadataRef, err := l.fetchMetadata(ctx, in.Metadata)
	if err != nil {
		return Handle{}, err
	}
	seriesDir, err := l.files.seriesDir(ref)
	if errors.Is(err, os.ErrNotExist) {
		return Handle{}, SeriesNotFoundError{Ref: ref}
	}
	if err != nil {
		return Handle{}, err
	}
	if _, err := os.Stat(fsroot.SeriesMetadataPath(seriesDir.Path())); err == nil {
		return Handle{}, SeriesAlreadyTrackedError{Ref: ref}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Handle{}, err
	}
	if err := l.checkMetadataAvailable(metadataRef, ref); err != nil {
		return Handle{}, err
	}
	series, err := buildSeries(metadataRef, metadataSeries, l.now())
	if err != nil {
		return Handle{}, err
	}
	if err := l.repo.save(ref, series); err != nil {
		return Handle{}, err
	}
	if err := l.index.Put(metadataRef, ref); err != nil {
		return Handle{}, err
	}
	if err := l.index.Save(); err != nil {
		return Handle{}, err
	}
	return Handle{lib: l, ref: ref}, nil
}

func (l *Library) Open(ref refs.Series) (Handle, error) {
	if _, err := l.files.seriesDir(ref); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Handle{}, SeriesNotFoundError{Ref: ref}
		}
		return Handle{}, err
	}
	if _, err := l.repo.load(ref); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Handle{}, SeriesNotTrackedError{Ref: ref}
		}
		return Handle{}, err
	}
	return Handle{lib: l, ref: ref}, nil
}

func (l *Library) Find(ref refs.Metadata) (Handle, error) {
	seriesRef, ok, err := l.index.Get(ref)
	if err != nil {
		return Handle{}, err
	}
	if !ok {
		return Handle{}, MetadataRefNotIndexedError{Ref: ref}
	}
	return l.Open(seriesRef)
}

func (l *Library) fetchMetadata(ctx context.Context, ref refs.Metadata) (metadata.Series, refs.Metadata, error) {
	if ref.Provider() == "" || ref.ID() == "" {
		return metadata.Series{}, "", fmt.Errorf("invalid metadata ref %q; expected <provider>:<id>", ref)
	}
	if ref.Provider() != l.source.Key() {
		return metadata.Series{}, "", UnsupportedMetadataSourceError{Source: ref.Provider()}
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
		return MetadataRefConflictError{Ref: metadataRef, Existing: existing, Next: next}
	}
	return nil
}

func buildSeries(ref refs.Metadata, metadataSeries metadata.Series, scannedAt time.Time) (Series, error) {
	out := Series{
		Metadata:    ref,
		LastScanned: scannedAt.UTC(),
		Episodes:    map[refs.Episode]Episode{},
	}
	var spine []SpineEpisode
	for _, season := range metadataSeries.Seasons {
		for _, episode := range season.Episodes {
			episodeRef, err := refs.NewEpisode(episode.SeasonNumber, episode.EpisodeNumber)
			if err != nil {
				return Series{}, err
			}
			spine = append(spine, SpineEpisode{Ref: episodeRef, AirDate: episode.Aired})
		}
	}
	editor{series: &out}.refreshSpine(spine)
	return out, nil
}

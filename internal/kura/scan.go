package kura

import (
	"context"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/ops"
	"github.com/wyvernzora/kura/internal/refs"
	seriespkg "github.com/wyvernzora/kura/internal/series"
	"github.com/wyvernzora/kura/internal/store"
)

func (s *Series) Scan(ctx context.Context, in ScanInput) (ScanResult, error) {
	if s.modern {
		handle, err := s.library.series.Open(refs.Series(s.ref))
		if err != nil {
			return ScanResult{}, normalizeSeriesLibraryError(err)
		}
		result, err := handle.Scan(ctx, seriespkg.ScanInput{Replace: in.Replace})
		if err != nil {
			return ScanResult{}, normalizeSeriesWorkflowError(s.ref, err)
		}
		out := ScanResult{
			Series:  s.ref,
			Synced:  make([]ScannedEpisode, 0, len(result.Synced)),
			Skipped: result.Skipped,
		}
		for _, entry := range result.Synced {
			out.Synced = append(out.Synced, ScannedEpisode{
				Status:     ScanStatus(entry.Status),
				Season:     entry.Season,
				Special:    entry.Special,
				Number:     entry.Number,
				Source:     entry.Source,
				Resolution: entry.Resolution,
				Path:       entry.Path,
				Companions: append([]string(nil), entry.Companions...),
			})
		}
		model, err := handle.Load()
		if err == nil {
			s.model = model
		}
		return out, nil
	}
	result, err := ops.SyncSeries(ctx, s.library.root, string(s.ref), ops.SeriesSyncOptions{
		MetadataResolver: s.metadataResolver(),
		Inspector:        s.library.inspector,
		Replace:          in.Replace,
	})
	if err != nil {
		return ScanResult{}, s.normalizeWorkflowError(err)
	}
	out := ScanResult{
		Series:  s.ref,
		Synced:  make([]ScannedEpisode, 0, len(result.Synced)),
		Skipped: result.Skipped,
	}
	for _, entry := range result.Synced {
		out.Synced = append(out.Synced, ScannedEpisode{
			Status:     ScanStatus(entry.Status),
			Season:     entry.Season,
			Special:    entry.Special,
			Number:     entry.Number,
			Source:     entry.Source,
			Resolution: entry.Resolution,
			Path:       entry.Path,
			Companions: append([]string(nil), entry.Companions...),
		})
	}
	if scanHasChanges(out) {
		seriesDir := s.library.root.Join(string(s.ref))
		if err := backupBefore(seriesDir, "series"); err != nil {
			return ScanResult{}, err
		}
		if err := store.SaveSeries(result.UpdatedSeries); err != nil {
			return ScanResult{}, err
		}
		if err := backupBefore(seriesDir, "trash"); err != nil {
			return ScanResult{}, err
		}
		if err := store.SaveTrash(result.UpdatedTrash); err != nil {
			return ScanResult{}, err
		}
		s.record = result.UpdatedSeries
	}
	return out, nil
}

func normalizeSeriesWorkflowError(ref SeriesRef, err error) error {
	exists, ok := errors.AsType[seriespkg.EpisodeAlreadyExistsError](err)
	if ok {
		return EpisodeAlreadyTrackedError{
			Series:  ref,
			Season:  exists.Season,
			Episode: exists.Episode,
		}
	}
	missing, ok := errors.AsType[seriespkg.MetadataMissingEpisodeError](err)
	if ok {
		return MetadataMissingEpisodeError{
			Series:  ref,
			Season:  missing.Season,
			Episode: missing.Episode,
		}
	}
	staged, ok := errors.AsType[seriespkg.StagedEpisodeAlreadyExistsError](err)
	if ok {
		return StagedEpisodeAlreadyExistsError{
			Series:  ref,
			Season:  staged.Season,
			Episode: staged.Episode,
		}
	}
	return normalizeSeriesLibraryError(err)
}

func (s *Series) metadataResolver() ops.MetadataSeriesResolver {
	return func(ctx context.Context, local store.Series) (metadata.Series, error) {
		return s.library.metadataSeriesForRecord(ctx, local)
	}
}

func scanHasChanges(result ScanResult) bool {
	for _, entry := range result.Synced {
		if entry.Status != ScanStatusExisting {
			return true
		}
	}
	return false
}

func (s *Series) normalizeWorkflowError(err error) error {
	exists, ok := errors.AsType[ops.EpisodeAlreadyExistsError](err)
	if ok {
		return EpisodeAlreadyTrackedError{
			Series:  s.ref,
			Season:  exists.Season,
			Episode: exists.Episode,
		}
	}
	stagedExists, ok := errors.AsType[ops.StagedEpisodeAlreadyExistsError](err)
	if ok {
		return StagedEpisodeAlreadyExistsError{
			Series:  s.ref,
			Season:  stagedExists.Season,
			Episode: stagedExists.Episode,
		}
	}
	missingEpisode, ok := errors.AsType[ops.MetadataMissingEpisodeError](err)
	if ok {
		return MetadataMissingEpisodeError{
			Series:  s.ref,
			Season:  missingEpisode.Season,
			Episode: missingEpisode.Episode,
		}
	}
	if errors.Is(err, ops.ErrSeriesNotTracked) {
		return SeriesNotTrackedError{Ref: s.ref}
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("kura: unknown workflow error")
}

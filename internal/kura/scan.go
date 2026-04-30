package kura

import (
	"context"
	"errors"

	"github.com/wyvernzora/kura/internal/refs"
	seriespkg "github.com/wyvernzora/kura/internal/series"
)

func (s *Series) Scan(ctx context.Context, in ScanInput) (ScanResult, error) {
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

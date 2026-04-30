package kura

import (
	"context"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/refs"
	seriespkg "github.com/wyvernzora/kura/internal/series"
)

func (s *Series) Stage(ctx context.Context, in StageInput) (StageResult, error) {
	episodeRef, err := refs.NewEpisode(in.Season, in.Episode)
	if err != nil {
		return StageResult{}, err
	}
	source := seriespkg.MediaSource("")
	if strings.TrimSpace(in.Source) != "" {
		source = seriespkg.ParseMediaSource(in.Source)
	}
	handle, err := s.library.series.Open(refs.Series(s.ref))
	if err != nil {
		return StageResult{}, normalizeSeriesLibraryError(err)
	}
	result, err := handle.Stage(ctx, seriespkg.StageInput{
		Episode:    episodeRef,
		MediaPath:  in.MediaPath,
		Source:     source.String(),
		Companions: append([]string(nil), in.Companions...),
		Replace:    in.Replace,
	})
	if err != nil {
		return StageResult{}, normalizeSeriesWorkflowError(s.ref, err)
	}
	model, loadErr := handle.Load()
	if loadErr == nil {
		s.model = model
	}
	return StageResult{
		Series:   s.ref,
		Applied:  true,
		Replaced: result.Replaced,
		Entry:    stagedEpisodeFromModern(result.Episode, result.Record),
	}, nil
}

func stagedEpisodeFromModern(ref refs.Episode, record seriespkg.MediaRecord) StagedEpisode {
	return StagedEpisode{
		Season: ref.Season(),
		Number: ref.Episode(),
		Episode: Episode{
			Number: ref.Episode(),
			Media: MediaFile{
				Path:   record.Path,
				Source: record.Source,
				Size:   record.Size,
				MTime:  record.MTime.UTC().Format(time.RFC3339),
				MediaInfo: &seriespkg.MediaInfo{
					Resolution: record.Resolution,
					VideoCodec: record.Codec,
				},
			},
			Companions: modernCompanions(record.Companions),
		},
	}
}

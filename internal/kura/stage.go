package kura

import (
	"context"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/ops"
	"github.com/wyvernzora/kura/internal/refs"
	seriespkg "github.com/wyvernzora/kura/internal/series"
	"github.com/wyvernzora/kura/internal/store"
)

func (s *Series) Stage(ctx context.Context, in StageInput) (StageResult, error) {
	season, err := domain.NewSeasonNumber(in.Season)
	if err != nil {
		return StageResult{}, err
	}
	episode, err := domain.NewEpisodeNumber(in.Episode)
	if err != nil {
		return StageResult{}, err
	}
	source := domain.MediaSource("")
	if strings.TrimSpace(in.Source) != "" {
		source = domain.ParseMediaSource(in.Source)
	}
	if s.modern {
		episodeRef, err := refs.NewEpisode(season.Int(), episode.Int())
		if err != nil {
			return StageResult{}, err
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
	result, err := ops.StageEpisodeFile(ctx, s.library.root, string(s.ref), ops.StageEpisodeFileOptions{
		Season:           season,
		Episode:          episode,
		Source:           source,
		Companions:       append([]string(nil), in.Companions...),
		MediaPath:        in.MediaPath,
		Inspector:        s.library.inspector,
		MetadataResolver: s.metadataResolver(),
		Replace:          in.Replace,
	})
	if err != nil {
		return StageResult{}, s.normalizeWorkflowError(err)
	}
	seriesDir := s.library.root.Join(string(s.ref))
	if err := backupBefore(seriesDir, "staged"); err != nil {
		return StageResult{}, err
	}
	if err := store.SaveStaged(result.UpdatedStaged); err != nil {
		return StageResult{}, err
	}
	return StageResult{
		Series:   s.ref,
		Applied:  true,
		Replaced: result.Replaced,
		Entry:    result.Entry,
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
				MediaInfo: &store.MediaInfo{
					Resolution: record.Resolution,
					VideoCodec: record.Codec,
				},
			},
			Companions: modernCompanions(record.Companions),
		},
	}
}

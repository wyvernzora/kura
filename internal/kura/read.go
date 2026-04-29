package kura

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/store"
)

const metadataDateLayout = "2006-01-02"

func (s *Series) Read(ctx context.Context, in ReadInput) (SeriesRead, error) {
	seriesDir, err := s.library.root.SeriesDir(string(s.ref))
	if err != nil {
		return SeriesRead{}, err
	}
	metadataSeries, err := s.metadataResolver()(ctx, s.record)
	if err != nil {
		return SeriesRead{}, err
	}
	staged, err := store.LoadStaged(seriesDir.Path())
	if err != nil {
		return SeriesRead{}, err
	}

	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	activeBySlot := activeEpisodeMap(s.record)
	stagedBySlot := stagedEpisodeMap(*staged)

	out := SeriesRead{
		MetadataRef:    MetadataRef(s.record.MetadataRef),
		Ref:            s.ref,
		Root:           seriesDir.Path(),
		PreferredTitle: metadataSeries.PreferredTitle,
		Seasons:        make([]SeasonRead, 0, len(metadataSeries.Seasons)),
	}
	if metadataSeries.CanonicalTitle != "" && metadataSeries.CanonicalTitle != metadataSeries.PreferredTitle {
		out.CanonicalTitle = metadataSeries.CanonicalTitle
	}
	for _, season := range metadataSeries.Seasons {
		out.Seasons = append(out.Seasons, readSeason(seriesDir, season, activeBySlot, stagedBySlot, now))
	}
	return out, nil
}

func readSeason(seriesDir fsroot.SeriesDir, season metadata.Season, activeBySlot map[string]store.Episode, stagedBySlot map[string]store.StagedEpisode, now time.Time) SeasonRead {
	out := SeasonRead{
		MetadataRef: season.MetadataRef,
		Number:      season.Number,
		Episodes:    make([]EpisodeRead, 0, len(season.Episodes)),
	}
	for _, episode := range season.Episodes {
		out.Episodes = append(out.Episodes, readEpisode(seriesDir, episode, activeBySlot, stagedBySlot, now))
	}
	return out
}

func readEpisode(seriesDir fsroot.SeriesDir, episode metadata.Episode, activeBySlot map[string]store.Episode, stagedBySlot map[string]store.StagedEpisode, now time.Time) EpisodeRead {
	key := episodeKey(episode.SeasonNumber, episode.EpisodeNumber)
	active, hasActive := activeBySlot[key]
	staged, hasStaged := stagedBySlot[key]

	out := EpisodeRead{
		MetadataRef:    episode.MetadataRef,
		Season:         episode.SeasonNumber,
		Number:         episode.EpisodeNumber,
		AbsoluteNumber: copyIntPtr(episode.AbsoluteNumber),
		Aired:          episode.Aired,
		Status:         episodeStatus(seriesDir, episode, active, hasActive, staged, hasStaged, now),
	}
	if hasActive {
		media := episodeMedia(active.Media, active.Companions)
		out.Active = &media
	}
	if hasStaged {
		media := episodeMedia(staged.Media, staged.Companions)
		out.Staged = &media
	}
	return out
}

func episodeStatus(seriesDir fsroot.SeriesDir, episode metadata.Episode, active store.Episode, hasActive bool, staged store.StagedEpisode, hasStaged bool, now time.Time) EpisodeStatus {
	if hasActive && mediaUnavailable(seriesDir, active.Media, false) {
		return EpisodeStatusUnavailable
	}
	if hasStaged && mediaUnavailable(seriesDir, staged.Media, true) {
		return EpisodeStatusUnavailable
	}
	if hasStaged {
		return EpisodeStatusStaged
	}
	if hasActive {
		return EpisodeStatusPresent
	}
	if isPendingEpisode(episode.Aired, now) {
		return EpisodeStatusPending
	}
	return EpisodeStatusMissing
}

func activeEpisodeMap(series store.Series) map[string]store.Episode {
	out := map[string]store.Episode{}
	for _, season := range series.Seasons {
		for _, episode := range season.Episodes {
			if episode.Media.Path == "" {
				continue
			}
			out[episodeKey(season.Number, episode.Number)] = episode
		}
	}
	return out
}

func stagedEpisodeMap(staged store.Staged) map[string]store.StagedEpisode {
	out := map[string]store.StagedEpisode{}
	for _, episode := range staged.Entries {
		if episode.Media.Path == "" {
			continue
		}
		out[episodeKey(episode.Season, episode.Number)] = episode
	}
	return out
}

func episodeMedia(media store.MediaFile, companions []store.CompanionFile) EpisodeMedia {
	return EpisodeMedia{
		Source:     domain.ParseMediaSource(media.Source).Display(),
		Resolution: mediaResolution(media.MediaInfo),
		File:       media.Path,
		Companions: copyCompanions(companions),
	}
}

func mediaResolution(info *store.MediaInfo) string {
	if info == nil {
		return ""
	}
	raw := strings.TrimSpace(info.Resolution)
	if raw == "" {
		return ""
	}
	resolution, err := domain.ParseResolution(raw)
	if err != nil || !resolution.Known() {
		return raw
	}
	return resolution.Display()
}

func mediaUnavailable(seriesDir fsroot.SeriesDir, media store.MediaFile, absolute bool) bool {
	path := media.Path
	if !absolute {
		joined, err := seriesDir.JoinRel(media.Path)
		if err != nil {
			return true
		}
		path = joined
	} else if !filepath.IsAbs(path) {
		return true
	}
	info, err := os.Stat(path)
	return err != nil || info.IsDir()
}

func isPendingEpisode(aired string, now time.Time) bool {
	aired = strings.TrimSpace(aired)
	if aired == "" {
		return false
	}
	airedDate, err := time.Parse(metadataDateLayout, aired)
	if err != nil {
		return false
	}
	today, err := time.Parse(metadataDateLayout, now.Format(metadataDateLayout))
	if err != nil {
		return false
	}
	return airedDate.After(today)
}

func copyIntPtr(in *int) *int {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func copyCompanions(in []store.CompanionFile) []store.CompanionFile {
	out := append([]store.CompanionFile(nil), in...)
	if out == nil {
		return []store.CompanionFile{}
	}
	return out
}

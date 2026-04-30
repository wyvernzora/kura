package kura

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/refs"
	seriespkg "github.com/wyvernzora/kura/internal/series"
)

const metadataDateLayout = "2006-01-02"

func (s *Series) Read(ctx context.Context, in ReadInput) (SeriesRead, error) {
	_ = ctx
	return s.readModern(in)
}

func (s *Series) readModern(in ReadInput) (SeriesRead, error) {
	seriesDir, err := seriespkg.ParseSeriesDir(s.library.root.Join(string(s.ref)))
	if err != nil {
		return SeriesRead{}, err
	}
	model := s.model
	if s.library.series != nil {
		if handle, err := s.library.series.Open(refs.Series(s.ref)); err == nil {
			if loaded, err := handle.Load(); err == nil {
				model = loaded
				s.model = loaded
			}
		}
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	out := SeriesRead{
		MetadataRef:    MetadataRef(model.Metadata),
		Ref:            s.ref,
		Root:           seriesDir.Path(),
		PreferredTitle: s.ref.String(),
		Seasons:        modernSeasonReads(seriesDir, model, now),
	}
	return out, nil
}

func modernSeasonReads(seriesDir seriespkg.SeriesDir, model seriespkg.Series, now time.Time) []SeasonRead {
	seasons := map[int][]EpisodeRead{}
	for ref, episode := range model.Episodes {
		read := EpisodeRead{
			Season: ref.Season(),
			Number: ref.Episode(),
			Aired:  episode.AirDate,
			Status: modernEpisodeStatus(seriesDir, episode, now),
		}
		if episode.Active != nil {
			media := modernEpisodeMedia(*episode.Active)
			read.Active = &media
		}
		if episode.Staged != nil {
			media := modernEpisodeMedia(*episode.Staged)
			read.Staged = &media
		}
		seasons[ref.Season()] = append(seasons[ref.Season()], read)
	}
	numbers := make([]int, 0, len(seasons))
	for number := range seasons {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	out := make([]SeasonRead, 0, len(numbers))
	for _, number := range numbers {
		episodes := seasons[number]
		sort.Slice(episodes, func(i, j int) bool { return episodes[i].Number < episodes[j].Number })
		out = append(out, SeasonRead{Number: number, Episodes: episodes})
	}
	return out
}

func modernEpisodeStatus(seriesDir seriespkg.SeriesDir, episode seriespkg.Episode, now time.Time) EpisodeStatus {
	if episode.Active != nil && modernMediaUnavailable(seriesDir, *episode.Active, false) {
		return EpisodeStatusUnavailable
	}
	if episode.Staged != nil && modernMediaUnavailable(seriesDir, *episode.Staged, true) {
		return EpisodeStatusUnavailable
	}
	if episode.Staged != nil {
		return EpisodeStatusStaged
	}
	if episode.Active != nil {
		return EpisodeStatusPresent
	}
	if isPendingEpisode(episode.AirDate, now) {
		return EpisodeStatusPending
	}
	return EpisodeStatusMissing
}

func modernEpisodeMedia(record seriespkg.MediaRecord) EpisodeMedia {
	return EpisodeMedia{
		Source:     seriespkg.ParseMediaSource(record.Source).Display(),
		Resolution: displayResolution(record.Resolution),
		File:       record.Path,
		Companions: modernCompanions(record.Companions),
	}
}

func displayResolution(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	resolution, err := seriespkg.ParseResolution(raw)
	if err != nil || !resolution.Known() {
		return raw
	}
	return resolution.Display()
}

func modernMediaUnavailable(seriesDir seriespkg.SeriesDir, media seriespkg.MediaRecord, absolute bool) bool {
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

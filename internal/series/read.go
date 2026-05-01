package series

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/textnorm"
)

const metadataDateLayout = "2006-01-02"

type ReadInput struct {
	Now time.Time
}

type EpisodeStatus string

const (
	EpisodeStatusPending     EpisodeStatus = "pending"
	EpisodeStatusMissing     EpisodeStatus = "missing"
	EpisodeStatusPresent     EpisodeStatus = "present"
	EpisodeStatusStaged      EpisodeStatus = "staged"
	EpisodeStatusUnavailable EpisodeStatus = "unavailable"
)

func (h Handle) Read(ctx context.Context, in ReadInput) (Series, error) {
	_ = ctx
	seriesDir, err := h.files().seriesDir(h.ref)
	if err != nil {
		return Series{}, err
	}
	model, err := h.load()
	if err != nil {
		return Series{}, err
	}
	now := in.Now
	if now.IsZero() {
		now = h.now()
	}
	return Series{
		MetadataRef:    model.Metadata,
		Ref:            h.ref,
		Root:           seriesDir.Path(),
		PreferredTitle: textnorm.NFC(h.ref.String()),
		Seasons:        seasonViews(seriesDir, model, now),
	}, nil
}

func seasonViews(seriesDir SeriesDir, model seriesState, now time.Time) []Season {
	seasons := map[int][]Episode{}
	for ref, episode := range model.Episodes {
		view := Episode{
			Episode: ref,
			Aired:   episode.AirDate,
			Status:  episodeStatus(seriesDir, episode, now),
		}
		if episode.Active != nil {
			media := episodeMedia(*episode.Active)
			view.Active = &media
		}
		if episode.Staged != nil {
			media := episodeMedia(*episode.Staged)
			view.Staged = &media
		}
		seasons[ref.Season()] = append(seasons[ref.Season()], view)
	}
	numbers := make([]int, 0, len(seasons))
	for number := range seasons {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	out := make([]Season, 0, len(numbers))
	for _, number := range numbers {
		episodes := seasons[number]
		sort.Slice(episodes, func(i, j int) bool { return episodes[i].Episode.Episode() < episodes[j].Episode.Episode() })
		out = append(out, Season{Number: number, Episodes: episodes})
	}
	return out
}

func episodeStatus(seriesDir SeriesDir, episode episodeState, now time.Time) EpisodeStatus {
	if episode.Active != nil && mediaUnavailable(seriesDir, *episode.Active, false) {
		return EpisodeStatusUnavailable
	}
	if episode.Staged != nil && mediaUnavailable(seriesDir, *episode.Staged, true) {
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

func episodeMedia(record MediaRecord) EpisodeMedia {
	return EpisodeMedia{
		Source:     ParseMediaSource(record.Source).Display(),
		Resolution: displayResolution(record.Resolution),
		File:       record.Path,
		Companions: companionFiles(record.Companions),
	}
}

func displayResolution(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	resolution, err := ParseResolution(raw)
	if err != nil || !resolution.Known() {
		return raw
	}
	return resolution.Display()
}

func mediaUnavailable(seriesDir SeriesDir, media MediaRecord, absolute bool) bool {
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

func companionFiles(in []CompanionRecord) []CompanionFile {
	out := make([]CompanionFile, 0, len(in))
	for _, companion := range in {
		out = append(out, CompanionFile{
			Path:     companion.Path,
			Role:     companion.Role,
			Language: companion.Language,
			Label:    companion.Label,
			Size:     companion.Size,
			MTime:    companion.MTime.UTC().Format(time.RFC3339),
		})
	}
	if out == nil {
		return []CompanionFile{}
	}
	return out
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

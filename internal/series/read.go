package series

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/textnorm"
)

const metadataDateLayout = "2006-01-02"

type ReadInput struct {
	Now time.Time
}

type ReadResult struct {
	MetadataRef    refs.Metadata       `json:"metadataRef"`
	Ref            refs.Series         `json:"ref"`
	Root           string              `json:"root"`
	PreferredTitle textnorm.NFCString  `json:"preferredTitle"`
	CanonicalTitle *textnorm.NFCString `json:"canonicalTitle,omitempty"`
	Seasons        []SeasonRead        `json:"seasons"`
}

type SeasonRead struct {
	MetadataRef refs.Metadata `json:"metadataRef,omitempty"`
	Number      int           `json:"number"`
	Episodes    []EpisodeRead `json:"episodes"`
}

type EpisodeRead struct {
	MetadataRef    refs.Metadata `json:"metadataRef,omitempty"`
	Season         int           `json:"season"`
	Number         int           `json:"number"`
	AbsoluteNumber *int          `json:"absoluteNumber,omitempty"`
	Aired          string        `json:"aired,omitempty"`
	Status         EpisodeStatus `json:"status"`
	Active         *EpisodeMedia `json:"active,omitempty"`
	Staged         *EpisodeMedia `json:"staged,omitempty"`
}

type EpisodeMedia struct {
	Source     string          `json:"source"`
	Resolution string          `json:"resolution,omitempty"`
	File       string          `json:"file"`
	Companions []CompanionFile `json:"companions"`
}

type CompanionFile struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}

type EpisodeStatus string

const (
	EpisodeStatusPending     EpisodeStatus = "pending"
	EpisodeStatusMissing     EpisodeStatus = "missing"
	EpisodeStatusPresent     EpisodeStatus = "present"
	EpisodeStatusStaged      EpisodeStatus = "staged"
	EpisodeStatusUnavailable EpisodeStatus = "unavailable"
)

func (h Handle) Read(ctx context.Context, in ReadInput) (ReadResult, error) {
	_ = ctx
	seriesDir, err := h.files().seriesDir(h.ref)
	if err != nil {
		return ReadResult{}, err
	}
	model, err := h.Load()
	if err != nil {
		return ReadResult{}, err
	}
	now := in.Now
	if now.IsZero() {
		now = h.now()
	}
	return ReadResult{
		MetadataRef:    model.Metadata,
		Ref:            h.ref,
		Root:           seriesDir.Path(),
		PreferredTitle: textnorm.NFC(h.ref.String()),
		Seasons:        seasonReads(seriesDir, model, now),
	}, nil
}

func seasonReads(seriesDir SeriesDir, model Series, now time.Time) []SeasonRead {
	seasons := map[int][]EpisodeRead{}
	for ref, episode := range model.Episodes {
		read := EpisodeRead{
			Season: ref.Season(),
			Number: ref.Episode(),
			Aired:  episode.AirDate,
			Status: episodeStatus(seriesDir, episode, now),
		}
		if episode.Active != nil {
			media := episodeMedia(*episode.Active)
			read.Active = &media
		}
		if episode.Staged != nil {
			media := episodeMedia(*episode.Staged)
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

func episodeStatus(seriesDir SeriesDir, episode Episode, now time.Time) EpisodeStatus {
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

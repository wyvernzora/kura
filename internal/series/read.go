package series

import (
	"context"
	"errors"
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
	EpisodeStatusPending           EpisodeStatus = "pending"
	EpisodeStatusMissing           EpisodeStatus = "missing"
	EpisodeStatusPresent           EpisodeStatus = "present"
	EpisodeStatusStaged            EpisodeStatus = "staged"
	EpisodeStatusStagedReplacement EpisodeStatus = "staged_replacement"
	EpisodeStatusUnavailable       EpisodeStatus = "unavailable"
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
	preferredTitle := model.PreferredTitle
	if preferredTitle.IsZero() {
		preferredTitle = textnorm.NFC(h.ref.String())
	}
	var canonicalTitle *textnorm.NFCString
	if !model.CanonicalTitle.IsZero() {
		title := model.CanonicalTitle
		canonicalTitle = &title
	}
	return Series{
		MetadataRef:    model.Metadata,
		Ref:            h.ref,
		Root:           seriesDir.Path(),
		LastScanned:    formatOptionalTime(model.LastScanned),
		PreferredTitle: preferredTitle,
		CanonicalTitle: canonicalTitle,
		Seasons:        seasonViews(seriesDir, model, now),
	}, nil
}

func seasonViews(seriesDir SeriesDir, model seriesState, now time.Time) []Season {
	seasons := map[int][]Episode{}
	for ref, episode := range model.Episodes {
		view := Episode{
			Episode:         ref,
			Aired:           episode.AirDate,
			Status:          episodeStatus(seriesDir, episode, now),
			Inconsistencies: episodeFilesystemIssues(seriesDir, episode),
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
	if episode.Active != nil && episode.Staged != nil {
		return EpisodeStatusStagedReplacement
	}
	if episode.Active != nil && len(pathFilesystemIssue(seriesDir, "active", "media", episode.Active.Path, false)) > 0 {
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

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
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

func episodeFilesystemIssues(seriesDir SeriesDir, episode episodeState) []FilesystemIssue {
	var issues []FilesystemIssue
	if episode.Active != nil {
		issues = append(issues, recordFilesystemIssues(seriesDir, "active", *episode.Active, false)...)
	}
	if episode.Staged != nil {
		issues = append(issues, recordFilesystemIssues(seriesDir, "staged", *episode.Staged, true)...)
	}
	return issues
}

func recordFilesystemIssues(seriesDir SeriesDir, recordName string, media MediaRecord, absolute bool) []FilesystemIssue {
	var issues []FilesystemIssue
	issues = append(issues, pathFilesystemIssue(seriesDir, recordName, "media", media.Path, absolute)...)
	for _, companion := range media.Companions {
		issues = append(issues, pathFilesystemIssue(seriesDir, recordName, "companion", companion.Path, absolute)...)
	}
	return issues
}

func pathFilesystemIssue(seriesDir SeriesDir, recordName string, kind string, rawPath string, absolute bool) []FilesystemIssue {
	path := rawPath
	if !absolute {
		joined, err := seriesDir.JoinRel(rawPath)
		if err != nil {
			return []FilesystemIssue{{
				Record: recordName,
				Path:   rawPath,
				Code:   recordName + "_" + kind + "_invalid_path",
				Reason: err.Error(),
			}}
		}
		path = joined
	} else if !filepath.IsAbs(path) {
		return []FilesystemIssue{{
			Record: recordName,
			Path:   rawPath,
			Code:   recordName + "_" + kind + "_invalid_path",
			Reason: "path must be absolute",
		}}
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return []FilesystemIssue{{
			Record: recordName,
			Path:   rawPath,
			Code:   recordName + "_" + kind + "_missing",
			Reason: "path does not exist",
		}}
	}
	if err != nil {
		return []FilesystemIssue{{
			Record: recordName,
			Path:   rawPath,
			Code:   recordName + "_" + kind + "_stat_error",
			Reason: err.Error(),
		}}
	}
	if info.IsDir() {
		return []FilesystemIssue{{
			Record: recordName,
			Path:   rawPath,
			Code:   recordName + "_" + kind + "_not_file",
			Reason: "path is a directory",
		}}
	}
	return nil
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

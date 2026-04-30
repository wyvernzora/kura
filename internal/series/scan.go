package series

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series/wire"
)

type ScanInput struct {
	Replace bool
}

type ScanResult struct {
	Series  refs.Series
	Synced  []ScannedEpisode
	Skipped []ImportSkip
}

type ImportSkip struct {
	Path   string `json:"path"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

const (
	SkipCodeSpecialNumberNotInferred = "special_number_not_inferred"
	SkipCodeEpisodeNumberNotInferred = "episode_number_not_inferred"
	SkipCodeSeasonMismatch           = "season_mismatch"
	SkipCodeIgnoredDirectory         = "ignored_directory"
)

type ScannedEpisode struct {
	Status     ScanStatus
	Season     int
	Special    bool
	Number     int
	Source     string
	Resolution string
	Path       string
	Companions []string
}

type ScanStatus string

const (
	ScanStatusNew      ScanStatus = "new"
	ScanStatusReplaced ScanStatus = "replaced"
	ScanStatusUpdated  ScanStatus = "updated"
	ScanStatusExisting ScanStatus = "existing"
)

type EpisodeAlreadyExistsError struct {
	Season  int
	Episode int
}

func (err EpisodeAlreadyExistsError) Error() string {
	return fmt.Sprintf("episode S%02dE%02d already exists; pass replace to replace it", err.Season, err.Episode)
}

type MetadataMissingEpisodeError struct {
	Season  int
	Episode int
}

func (err MetadataMissingEpisodeError) Error() string {
	return fmt.Sprintf("metadata has no S%02dE%02d", err.Season, err.Episode)
}

func (h Handle) Scan(ctx context.Context, in ScanInput) (ScanResult, error) {
	series, err := h.Load()
	if err != nil {
		return ScanResult{}, err
	}
	metadataSeries, err := h.source().GetSeries(ctx, series.Metadata.ID())
	if err != nil {
		return ScanResult{}, err
	}
	spine, err := spineFromMetadata(metadataSeries.Seasons)
	if err != nil {
		return ScanResult{}, err
	}
	editor := editor{series: &series}
	editor.refreshSpine(spine)

	seriesDir, err := h.files().seriesDir(h.ref)
	if err != nil {
		return ScanResult{}, err
	}
	discovered, skipped, err := discoverSeriesEpisodes(seriesDir)
	if err != nil {
		return ScanResult{}, err
	}

	result := ScanResult{Series: h.ref, Skipped: skipped}
	for _, file := range discovered {
		episode, ok := series.Episodes[file.Ref]
		if !ok {
			return ScanResult{}, MetadataMissingEpisodeError{Season: file.Ref.Season(), Episode: file.Ref.Episode()}
		}
		status := ScanStatusNew
		if episode.Active != nil {
			if episode.Active.Path != file.Path {
				if !in.Replace {
					return ScanResult{}, EpisodeAlreadyExistsError{Season: file.Ref.Season(), Episode: file.Ref.Episode()}
				}
				status = ScanStatusReplaced
			} else if unchanged, err := h.unchanged(seriesDir, *episode.Active, file); err != nil {
				return ScanResult{}, err
			} else if unchanged {
				result.Synced = append(result.Synced, scanEntry(statusExisting(file, *episode.Active)))
				continue
			} else {
				status = ScanStatusUpdated
			}
		}
		record, err := h.mediaRecord(ctx, seriesDir, file)
		if err != nil {
			return ScanResult{}, err
		}
		if err := editor.setActive(file.Ref, record); err != nil {
			return ScanResult{}, err
		}
		result.Synced = append(result.Synced, ScannedEpisode{
			Status:     status,
			Season:     file.Ref.Season(),
			Special:    file.Ref.Season() == 0,
			Number:     file.Ref.Episode(),
			Source:     ParseMediaSource(record.Source).Display(),
			Resolution: record.Resolution,
			Path:       record.Path,
			Companions: append([]string(nil), file.Companions...),
		})
	}
	series.LastScanned = h.now().UTC()
	if err := h.repo().save(h.ref, series); err != nil {
		return ScanResult{}, err
	}
	return result, nil
}

func spineFromMetadata(seasons []metadata.Season) ([]SpineEpisode, error) {
	var spine []SpineEpisode
	for _, season := range seasons {
		for _, episode := range season.Episodes {
			ref, err := refs.NewEpisode(episode.SeasonNumber, episode.EpisodeNumber)
			if err != nil {
				return nil, err
			}
			spine = append(spine, SpineEpisode{Ref: ref, AirDate: episode.Aired})
		}
	}
	return spine, nil
}

type discoveredFile struct {
	Ref        refs.Episode
	Path       string
	Source     string
	Companions []string
}

func discoverSeriesEpisodes(seriesDir SeriesDir) ([]discoveredFile, []ImportSkip, error) {
	entries, err := os.ReadDir(seriesDir.Path())
	if err != nil {
		return nil, nil, err
	}

	var episodes []discoveredFile
	var skipped []ImportSkip
	for _, entry := range entries {
		name := entry.Name()
		if name == wire.KuraDir {
			continue
		}
		fullPath := filepath.Join(seriesDir.Path(), name)
		if entry.IsDir() {
			season, ok := parseSeasonDir(name)
			if !ok {
				skipped = append(skipped, ImportSkip{Path: filepath.ToSlash(name), Code: SkipCodeIgnoredDirectory, Reason: "directory is not a season directory"})
				continue
			}
			discovered, seasonSkipped, err := discoverSeasonEpisodes(seriesDir, fullPath, season)
			if err != nil {
				return nil, nil, err
			}
			episodes = append(episodes, discovered...)
			skipped = append(skipped, seasonSkipped...)
			continue
		}
		relPath := filepath.ToSlash(name)
		if !recognizedVideoFile(relPath) {
			continue
		}
		season, number, ok := inferEpisodeFromFilename(name)
		if !ok || season != 0 {
			skipped = append(skipped, ImportSkip{Path: relPath, Code: SkipCodeSpecialNumberNotInferred, Reason: "could not infer special number"})
			continue
		}
		ref, err := refs.NewEpisode(0, number)
		if err != nil {
			return nil, nil, err
		}
		episodes = append(episodes, discoveredFile{
			Ref:        ref,
			Path:       relPath,
			Source:     ParseMediaSource(inferSourceFromFilename(relPath)).String(),
			Companions: matchingCompanions(seriesDir.Path(), "", name, entries),
		})
	}
	sortDiscoveredEpisodes(episodes)
	return episodes, skipped, nil
}

func discoverSeasonEpisodes(seriesDir SeriesDir, seasonDir string, season int) ([]discoveredFile, []ImportSkip, error) {
	entries, err := os.ReadDir(seasonDir)
	if err != nil {
		return nil, nil, err
	}
	var episodes []discoveredFile
	var skipped []ImportSkip
	for _, entry := range entries {
		if entry.IsDir() {
			fullPath := filepath.Join(seasonDir, entry.Name())
			relPath, err := filepath.Rel(seriesDir.Path(), fullPath)
			if err != nil {
				return nil, nil, err
			}
			skipped = append(skipped, ImportSkip{Path: filepath.ToSlash(relPath), Code: SkipCodeIgnoredDirectory, Reason: "season subdirectory is not scanned"})
			continue
		}
		fullPath := filepath.Join(seasonDir, entry.Name())
		relPath, err := filepath.Rel(seriesDir.Path(), fullPath)
		if err != nil {
			return nil, nil, err
		}
		relPath = filepath.ToSlash(relPath)
		if !recognizedVideoFile(relPath) {
			continue
		}
		inferredSeason, number, ok := inferEpisodeFromFilename(entry.Name())
		if !ok {
			skipped = append(skipped, ImportSkip{Path: relPath, Code: SkipCodeEpisodeNumberNotInferred, Reason: "could not infer episode number"})
			continue
		}
		if inferredSeason > 0 && inferredSeason != season {
			skipped = append(skipped, ImportSkip{Path: relPath, Code: SkipCodeSeasonMismatch, Reason: "filename season does not match directory season"})
			continue
		}
		ref, err := refs.NewEpisode(season, number)
		if err != nil {
			return nil, nil, err
		}
		episodes = append(episodes, discoveredFile{
			Ref:        ref,
			Path:       relPath,
			Source:     ParseMediaSource(inferSourceFromFilename(relPath)).String(),
			Companions: matchingCompanions(seriesDir.Path(), seasonDir, entry.Name(), entries),
		})
	}
	return episodes, skipped, nil
}

func matchingCompanions(seriesDir string, dir string, videoName string, entries []os.DirEntry) []string {
	videoBase := strings.TrimSuffix(videoName, filepath.Ext(videoName))
	var companions []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == videoName {
			continue
		}
		name := entry.Name()
		if recognizedVideoFile(name) {
			continue
		}
		companionBase := strings.TrimSuffix(name, filepath.Ext(name))
		if companionBase != videoBase && !strings.HasPrefix(name, videoBase+".") {
			continue
		}
		fullPath := filepath.Join(dir, name)
		if dir == "" {
			fullPath = filepath.Join(seriesDir, name)
		}
		relPath, err := filepath.Rel(seriesDir, fullPath)
		if err != nil {
			continue
		}
		companions = append(companions, filepath.ToSlash(relPath))
	}
	sort.Strings(companions)
	return companions
}

func sortDiscoveredEpisodes(episodes []discoveredFile) {
	sort.Slice(episodes, func(i, j int) bool {
		if episodes[i].Ref.Season() != episodes[j].Ref.Season() {
			return episodes[i].Ref.Season() < episodes[j].Ref.Season()
		}
		if episodes[i].Ref.Episode() != episodes[j].Ref.Episode() {
			return episodes[i].Ref.Episode() < episodes[j].Ref.Episode()
		}
		return episodes[i].Path < episodes[j].Path
	})
}

func (h Handle) unchanged(seriesDir SeriesDir, active MediaRecord, file discoveredFile) (bool, error) {
	facts, err := h.files().stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(file.Path)))
	if err != nil {
		return false, err
	}
	if active.Size != facts.Size || !active.MTime.Equal(facts.MTime) {
		return false, nil
	}
	if len(active.Companions) != len(file.Companions) {
		return false, nil
	}
	companions := map[string]CompanionRecord{}
	for _, companion := range active.Companions {
		companions[companion.Path] = companion
	}
	for _, path := range file.Companions {
		companion, ok := companions[path]
		if !ok {
			return false, nil
		}
		facts, err := h.files().stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(path)))
		if err != nil {
			return false, nil
		}
		if companion.Size != facts.Size || !companion.MTime.Equal(facts.MTime) {
			return false, nil
		}
	}
	return true, nil
}

func (h Handle) mediaRecord(ctx context.Context, seriesDir SeriesDir, file discoveredFile) (MediaRecord, error) {
	absolutePath := filepath.Join(seriesDir.Path(), filepath.FromSlash(file.Path))
	info, err := h.inspector().Inspect(ctx, absolutePath)
	if err != nil {
		return MediaRecord{}, err
	}
	facts, err := h.files().stat(absolutePath)
	if err != nil {
		return MediaRecord{}, err
	}
	record := MediaRecord{
		Path:       file.Path,
		Source:     ParseMediaSource(file.Source).String(),
		Resolution: info.Resolution,
		Codec:      info.VideoCodec,
		Size:       facts.Size,
		MTime:      facts.MTime,
		Companions: []CompanionRecord{},
	}
	for _, companionPath := range file.Companions {
		facts, err := h.files().stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(companionPath)))
		if err != nil {
			return MediaRecord{}, err
		}
		record.Companions = append(record.Companions, CompanionRecord{
			Path:  companionPath,
			Size:  facts.Size,
			MTime: facts.MTime,
		})
	}
	return record, nil
}

func statusExisting(file discoveredFile, active MediaRecord) ScannedEpisode {
	return ScannedEpisode{
		Status:     ScanStatusExisting,
		Season:     file.Ref.Season(),
		Special:    file.Ref.Season() == 0,
		Number:     file.Ref.Episode(),
		Source:     ParseMediaSource(active.Source).Display(),
		Resolution: active.Resolution,
		Path:       active.Path,
		Companions: append([]string(nil), file.Companions...),
	}
}

func scanEntry(entry ScannedEpisode) ScannedEpisode {
	return entry
}

func IsNotTracked(err error) bool {
	var notTracked SeriesNotTrackedError
	return errors.As(err, &notTracked)
}

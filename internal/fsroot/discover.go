package fsroot

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DiscoveredEpisode struct {
	Season     int
	Special    bool
	Number     int
	Path       string
	Source     string
	Companions []string
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

func DiscoverSeriesEpisodes(seriesDir SeriesDir) ([]DiscoveredEpisode, []ImportSkip, error) {
	entries, err := os.ReadDir(seriesDir.Path())
	if err != nil {
		return nil, nil, err
	}

	var episodes []DiscoveredEpisode
	var skipped []ImportSkip
	for _, entry := range entries {
		name := entry.Name()
		if name == KuraDir {
			continue
		}
		fullPath := filepath.Join(seriesDir.Path(), name)
		if entry.IsDir() {
			season, ok := ParseSeasonDir(name)
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
		if !RecognizedVideoFile(relPath) {
			continue
		}
		season, number, ok := InferEpisodeFromFilename(name)
		if !ok || season != 0 {
			skipped = append(skipped, ImportSkip{Path: relPath, Code: SkipCodeSpecialNumberNotInferred, Reason: "could not infer special number"})
			continue
		}
		episodes = append(episodes, DiscoveredEpisode{
			Season:     0,
			Special:    true,
			Number:     number,
			Path:       relPath,
			Source:     InferSourceFromFilename(relPath).String(),
			Companions: matchingCompanions(seriesDir.Path(), "", name, entries),
		})
	}
	sortDiscoveredEpisodes(episodes)
	return episodes, skipped, nil
}

func discoverSeasonEpisodes(seriesDir SeriesDir, seasonDir string, season int) ([]DiscoveredEpisode, []ImportSkip, error) {
	entries, err := os.ReadDir(seasonDir)
	if err != nil {
		return nil, nil, err
	}
	var episodes []DiscoveredEpisode
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
		if !RecognizedVideoFile(relPath) {
			continue
		}
		inferredSeason, number, ok := InferEpisodeFromFilename(entry.Name())
		if !ok {
			skipped = append(skipped, ImportSkip{Path: relPath, Code: SkipCodeEpisodeNumberNotInferred, Reason: "could not infer episode number"})
			continue
		}
		if inferredSeason > 0 && inferredSeason != season {
			skipped = append(skipped, ImportSkip{Path: relPath, Code: SkipCodeSeasonMismatch, Reason: "filename season does not match directory season"})
			continue
		}
		episodes = append(episodes, DiscoveredEpisode{
			Season:     season,
			Special:    season == 0,
			Number:     number,
			Path:       relPath,
			Source:     InferSourceFromFilename(relPath).String(),
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
		if RecognizedVideoFile(name) {
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

func sortDiscoveredEpisodes(episodes []DiscoveredEpisode) {
	sort.Slice(episodes, func(i, j int) bool {
		if episodes[i].Season != episodes[j].Season {
			return episodes[i].Season < episodes[j].Season
		}
		if episodes[i].Number != episodes[j].Number {
			return episodes[i].Number < episodes[j].Number
		}
		return episodes[i].Path < episodes[j].Path
	})
}

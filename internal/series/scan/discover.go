package scan

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/series/mediarecord"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesdir"
)

type DiscoveredFile struct {
	Ref        refs.Episode
	Path       string
	Source     string
	Companions []string
}

func DiscoverSeriesEpisodes(seriesDir seriesdir.SeriesDir) ([]DiscoveredFile, []ImportSkip, error) {
	var episodes []DiscoveredFile
	var skipped []ImportSkip
	err := fs.WalkDir(os.DirFS(seriesDir.Path()), ".", func(relPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if relPath == "." {
			return nil
		}
		if entry.IsDir() {
			skip, reason := skipDiscoveryDir(relPath, entry.Name())
			if skip == nil {
				return nil
			}
			if reason != "" {
				skipped = append(skipped, ImportSkip{Path: relPath, Code: SkipCodeIgnoredDirectory, Reason: reason})
			}
			return skip
		}
		if !mediarecord.RecognizedVideoFile(relPath) {
			return nil
		}
		episode, skip, err := discoveredEpisode(seriesDir, relPath, entry.Name())
		if err != nil {
			return err
		}
		if skip != nil {
			skipped = append(skipped, *skip)
			return nil
		}
		episodes = append(episodes, episode)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sortDiscoveredEpisodes(episodes)
	return episodes, skipped, nil
}

func skipDiscoveryDir(relPath string, name string) (error, string) {
	if relPath == paths.KuraDir {
		return fs.SkipDir, ""
	}
	switch relPathDepth(relPath) {
	case 1:
		if _, ok := parseSeasonDir(name); ok {
			return nil, ""
		}
		return fs.SkipDir, "directory is not a season directory"
	default:
		if relPathDepth(relPath) == 2 && isSeasonExtraDir(relPath, name) {
			return fs.SkipDir, ""
		}
		return fs.SkipDir, "season subdirectory is not scanned"
	}
}

func isSeasonExtraDir(relPath string, name string) bool {
	seasonDir, _, ok := strings.Cut(relPath, "/")
	if !ok {
		return false
	}
	if _, ok := parseSeasonDir(seasonDir); !ok {
		return false
	}
	return strings.EqualFold(name, paths.ExtraDirName)
}

func discoveredEpisode(seriesDir seriesdir.SeriesDir, relPath string, name string) (DiscoveredFile, *ImportSkip, error) {
	switch relPathDepth(relPath) {
	case 1:
		return discoveredSpecial(seriesDir, relPath, name)
	case 2:
		return discoveredSeasonEpisode(seriesDir, relPath, name)
	default:
		return DiscoveredFile{}, nil, nil
	}
}

func discoveredSpecial(seriesDir seriesdir.SeriesDir, relPath string, name string) (DiscoveredFile, *ImportSkip, error) {
	season, number, ok := inferEpisodeFromFilename(name)
	if !ok || season != 0 {
		return DiscoveredFile{}, &ImportSkip{Path: relPath, Code: SkipCodeSpecialNumberNotInferred, Reason: "could not infer special number"}, nil
	}
	return discoveredFileFor(seriesDir, relPath, 0, number)
}

func discoveredSeasonEpisode(seriesDir seriesdir.SeriesDir, relPath string, name string) (DiscoveredFile, *ImportSkip, error) {
	seasonDir, _, _ := strings.Cut(relPath, "/")
	season, ok := parseSeasonDir(seasonDir)
	if !ok {
		return DiscoveredFile{}, nil, nil
	}
	inferredSeason, number, ok := inferEpisodeFromFilename(name)
	if !ok {
		return DiscoveredFile{}, &ImportSkip{Path: relPath, Code: SkipCodeEpisodeNumberNotInferred, Reason: "could not infer episode number"}, nil
	}
	if inferredSeason > 0 && inferredSeason != season {
		return DiscoveredFile{}, &ImportSkip{Path: relPath, Code: SkipCodeSeasonMismatch, Reason: "filename season does not match directory season"}, nil
	}
	return discoveredFileFor(seriesDir, relPath, season, number)
}

func discoveredFileFor(seriesDir seriesdir.SeriesDir, relPath string, season int, number int) (DiscoveredFile, *ImportSkip, error) {
	ref, err := refs.NewEpisode(season, number)
	if err != nil {
		return DiscoveredFile{}, nil, err
	}
	companions, err := matchingCompanions(seriesDir, path.Dir(relPath), path.Base(relPath))
	if err != nil {
		return DiscoveredFile{}, nil, err
	}
	return DiscoveredFile{
		Ref:        ref,
		Path:       relPath,
		Source:     media.ParseSource(mediarecord.InferSourceFromFilename(relPath)).String(),
		Companions: companions,
	}, nil, nil
}

func matchingCompanions(seriesDir seriesdir.SeriesDir, parentRel string, videoName string) ([]string, error) {
	parentDir := seriesDir.Path()
	if parentRel != "." {
		parentDir = filepath.Join(parentDir, filepath.FromSlash(parentRel))
	}
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return nil, err
	}
	videoBase := strings.TrimSuffix(videoName, filepath.Ext(videoName))
	var companions []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == videoName {
			continue
		}
		name := entry.Name()
		if mediarecord.RecognizedVideoFile(name) {
			continue
		}
		companionBase := strings.TrimSuffix(name, filepath.Ext(name))
		if companionBase != videoBase && !strings.HasPrefix(name, videoBase+".") {
			continue
		}
		relPath := name
		if parentRel != "." {
			relPath = path.Join(parentRel, name)
		}
		companions = append(companions, relPath)
	}
	sort.Strings(companions)
	return companions, nil
}

func sortDiscoveredEpisodes(episodes []DiscoveredFile) {
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

func relPathDepth(relPath string) int {
	if relPath == "." || relPath == "" {
		return 0
	}
	return strings.Count(relPath, "/") + 1
}

package scan

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wyvernzora/kura/services/library/internal/domain/media"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/mediainfo"
	"github.com/wyvernzora/kura/services/library/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library/internal/storage/seriesdir"
)

type DiscoveredFile struct {
	Ref        refs.Episode
	Path       string
	Source     string
	Companions []string
}

// DiscoverSeriesEpisodes walks seriesDir and returns the per-episode
// discovery result. Same as WalkSeriesEpisodes plus a duplicate-slot
// pass that drops colliding files from the kept list (re-emitting
// each as a SkipCodeDuplicateSlot entry). Use WalkSeriesEpisodes when
// you need to see every video including dupes.
func DiscoverSeriesEpisodes(seriesDir seriesdir.SeriesDir) ([]DiscoveredFile, []ImportSkip, error) {
	episodes, skipped, err := WalkSeriesEpisodes(seriesDir)
	if err != nil {
		return nil, nil, err
	}
	return rejectDuplicateSlots(seriesDir, episodes, skipped, nil)
}

// WalkSeriesEpisodes returns every recognized video under seriesDir
// with its parsed slot, source hint, and companions. Duplicate-slot
// detection is the caller's job — group via GroupBySlot if needed.
// Also returns ImportSkips for files / dirs the walk declined.
func WalkSeriesEpisodes(seriesDir seriesdir.SeriesDir) ([]DiscoveredFile, []ImportSkip, error) {
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
			descend, reason := classifyDirectory(relPath, entry.Name())
			if descend {
				return nil
			}
			if reason != "" {
				skipped = append(skipped, ImportSkip{Path: relPath, Code: SkipCodeIgnoredDirectory, Reason: reason})
			}
			return fs.SkipDir
		}
		if !mediainfo.RecognizedVideoFile(relPath) {
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

// GroupBySlot indexes a discovery result by episode ref. Useful for
// callers that need the per-slot file list (e.g. agents picking a
// winner among duplicates).
func GroupBySlot(files []DiscoveredFile) map[refs.Episode][]DiscoveredFile {
	out := make(map[refs.Episode][]DiscoveredFile, len(files))
	for _, f := range files {
		out[f.Ref] = append(out[f.Ref], f)
	}
	return out
}

// rejectDuplicateSlots removes any DiscoveredFile whose Ref collides
// with another file in the same scan and re-emits both as ImportSkips
// with SkipCodeDuplicateSlot. Each skip carries quality hints
// (source, resolution, size) so callers can pick a winner without
// re-walking the filesystem. Kura does not auto-pick.
func rejectDuplicateSlots(
	seriesDir seriesdir.SeriesDir,
	episodes []DiscoveredFile,
	skipped []ImportSkip,
	keep func(DiscoveredFile) bool,
) ([]DiscoveredFile, []ImportSkip, error) {
	byRef := map[refs.Episode][]int{}
	for index, episode := range episodes {
		byRef[episode.Ref] = append(byRef[episode.Ref], index)
	}
	if len(byRef) == len(episodes) {
		return episodes, skipped, nil
	}
	dropped := map[int]struct{}{}
	for ref, indices := range byRef {
		if len(indices) < 2 {
			continue
		}
		keepIndex := -1
		if keep != nil {
			for _, index := range indices {
				if keep(episodes[index]) {
					keepIndex = index
					break
				}
			}
		}
		for _, index := range indices {
			if index == keepIndex {
				continue
			}
			dropped[index] = struct{}{}
			skip := ImportSkip{
				Path:   episodes[index].Path,
				Code:   SkipCodeDuplicateSlot,
				Reason: fmt.Sprintf("multiple files claim %s; resolve manually", ref.Marker()),
			}
			enrichSkipQuality(seriesDir, &skip, episodes[index].Path, episodes[index].Source)
			skipped = append(skipped, skip)
		}
	}
	out := episodes[:0]
	for index, episode := range episodes {
		if _, drop := dropped[index]; drop {
			continue
		}
		out = append(out, episode)
	}
	return out, skipped, nil
}

// enrichSkipQuality fills source / resolution / size on a skip when
// the path is a recognized video file. Source is taken from the
// already-inferred DiscoveredFile.Source; resolution comes from the
// filename; size is statted from disk (0 on stat failure — quality
// hints are best-effort, never fatal).
func enrichSkipQuality(seriesDir seriesdir.SeriesDir, skip *ImportSkip, relPath, sourceHint string) {
	if !mediainfo.RecognizedVideoFile(relPath) {
		return
	}
	if sourceHint != "" && sourceHint != media.SourceUnknown.String() {
		skip.Source = sourceHint
	}
	if res := mediainfo.InferResolutionFromFilename(relPath); res != "" {
		skip.Resolution = res
	}
	abs := filepath.Join(seriesDir.Path(), filepath.FromSlash(relPath))
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		skip.Size = info.Size()
	}
}

// classifyDirectory decides whether discovery should descend into a
// directory. Returns descend=true to walk in, otherwise (false, reason)
// where reason is the human-readable skip note ("" for silent skips like
// .kura/ and Season N/Extra/).
func classifyDirectory(relPath, name string) (descend bool, reason string) {
	if relPath == paths.KuraDir {
		return false, ""
	}
	depth := relPathDepth(relPath)
	if depth == 1 {
		if _, ok := parseSeasonDir(name); ok {
			return true, ""
		}
		return false, "directory is not a season directory"
	}
	if depth == 2 && isSeasonExtraDir(relPath, name) {
		return false, ""
	}
	return false, "season subdirectory is not scanned"
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
		Source:     media.ParseSource(mediainfo.InferSourceFromFilename(relPath)).String(),
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
		if mediainfo.RecognizedVideoFile(name) {
			continue
		}
		// Companions match either "<videoBase>.<ext>" or
		// "<videoBase>.<lang>.<ext>"; both share the "<videoBase>." prefix.
		if !strings.HasPrefix(name, videoBase+".") {
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

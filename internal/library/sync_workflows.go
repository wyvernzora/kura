package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/progress"
)

type MediaInspector interface {
	Inspect(context.Context, string) (MediaInfo, error)
}

type SeriesSyncOptions struct {
	ProviderSeries          *metadata.Series
	PreserveFilesystemTitle bool
	Inspector               MediaInspector
	Apply                   bool
	DryRun                  bool
}

type SeriesSyncResult struct {
	Series        string            `json:"series"`
	Initialized   bool              `json:"initialized"`
	DryRun        bool              `json:"dryRun"`
	Synced        []SeriesSyncEntry `json:"synced"`
	Skipped       []ImportSkip      `json:"skipped"`
	UpdatedSeries Series            `json:"-"`
}

type SeriesSyncEntry struct {
	Status     string   `json:"status"`
	Season     int      `json:"season,omitempty"`
	Special    bool     `json:"special,omitempty"`
	Number     int      `json:"number"`
	Source     string   `json:"source"`
	Resolution string   `json:"resolution,omitempty"`
	Path       string   `json:"path"`
	Companions []string `json:"companions"`
}

func (l library) SyncSeries(ctx context.Context, root LibraryRoot, dirname string, opts SeriesSyncOptions) (SeriesSyncResult, error) {
	seriesDir, err := root.SeriesDir(dirname)
	if err != nil {
		return SeriesSyncResult{}, err
	}

	var initialized bool
	var series *Series
	if _, err := os.Stat(SeriesPath(seriesDir.Path())); err == nil {
		series, err = l.LoadSeries(seriesDir.Path())
		if err != nil {
			return SeriesSyncResult{}, err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if opts.ProviderSeries == nil {
			return SeriesSyncResult{}, fmt.Errorf("library: provider series is required to initialize %q", dirname)
		}
		series, err = newSeriesFromProvider(l, seriesDir.Path(), *opts.ProviderSeries)
		if err != nil {
			return SeriesSyncResult{}, err
		}
		if opts.PreserveFilesystemTitle {
			series.FilesystemTitle = CleanFilesystemTitle(dirname).String()
		}
		initialized = true
	} else {
		return SeriesSyncResult{}, err
	}

	discovered, skipped, err := DiscoverSeriesEpisodes(seriesDir)
	if err != nil {
		return SeriesSyncResult{}, err
	}

	updated := *series
	synced := make([]SeriesSyncEntry, 0, len(discovered))
	progress.Start(ctx, "series-sync", fmt.Sprintf("Found %d episode media file(s) for %s", len(discovered), seriesDir.Name()), len(discovered))
	for index, episode := range discovered {
		progress.Update(ctx, "series-sync", fmt.Sprintf("Inspecting %d/%d: %s", index+1, len(discovered), episode.Path), index+1, len(discovered))
		existing, ok, err := unchangedTrackedEpisode(seriesDir, updated, episode)
		if err != nil {
			return SeriesSyncResult{}, err
		}
		if ok {
			progress.Update(ctx, "series-sync", fmt.Sprintf("Keeping existing %d/%d: %s", index+1, len(discovered), episode.Path), index+1, len(discovered))
			synced = append(synced, existingSyncEntry(episode, existing))
			continue
		}
		if opts.Inspector == nil {
			return SeriesSyncResult{}, errors.New("library: media inspector is required")
		}
		mediaInfo, err := opts.Inspector.Inspect(ctx, filepath.Join(seriesDir.Path(), filepath.FromSlash(episode.Path)))
		if err != nil {
			progress.Failure(ctx, "series-sync", fmt.Sprintf("Failed inspecting %s", episode.Path), index+1, len(discovered))
			return SeriesSyncResult{}, err
		}
		progress.Update(ctx, "series-sync", fmt.Sprintf("Recording %d/%d: %s", index+1, len(discovered), episode.Path), index+1, len(discovered))
		next, err := AddEpisode(seriesDir.Path(), updated, AddEpisodeOptions{
			Season:     episode.Season,
			Episode:    episode.Number,
			Path:       episode.Path,
			Source:     episode.Source,
			Companions: episode.Companions,
			MediaInfo:  &mediaInfo,
		})
		if err != nil {
			progress.Failure(ctx, "series-sync", fmt.Sprintf("Failed recording %s", episode.Path), index+1, len(discovered))
			return SeriesSyncResult{}, err
		}
		updated = next
		synced = append(synced, SeriesSyncEntry{
			Status:     "new",
			Season:     episode.Season,
			Special:    episode.Special,
			Number:     episode.Number,
			Source:     ParseMediaSource(episode.Source).Display(),
			Resolution: mediaInfo.Resolution,
			Path:       episode.Path,
			Companions: episode.Companions,
		})
	}
	progress.Success(ctx, "series-sync", fmt.Sprintf("Processed %d episode media file(s); skipped %d", len(synced), len(skipped)), len(discovered))

	result := SeriesSyncResult{
		Series:        seriesDir.Name(),
		Initialized:   initialized,
		DryRun:        opts.DryRun,
		Synced:        synced,
		Skipped:       skipped,
		UpdatedSeries: updated,
	}
	if opts.Apply {
		progress.Start(ctx, "series-sync-write", fmt.Sprintf("Writing series metadata: %s", SeriesPath(seriesDir.Path())), 0)
		if err := l.SaveSeries(updated); err != nil {
			progress.Failure(ctx, "series-sync-write", "Failed writing series metadata", 0, 0)
			return SeriesSyncResult{}, err
		}
		progress.Success(ctx, "series-sync-write", fmt.Sprintf("Synced %d episode media file(s)", len(synced)), len(synced))
	}
	return result, nil
}

type ImportEpisodeFileOptions struct {
	Season     SeasonNumber
	Episode    EpisodeNumber
	Source     MediaSource
	Companions []string
	MediaPath  string
	Inspector  MediaInspector
	Apply      bool
}

func (l library) ImportEpisodeFile(ctx context.Context, root LibraryRoot, opts ImportEpisodeFileOptions) (Series, error) {
	if opts.Episode.Int() < 1 {
		return Series{}, errors.New("library: episode number must be greater than zero")
	}
	mediaPath, err := cleanLibraryRelativePath(opts.MediaPath)
	if err != nil {
		return Series{}, err
	}
	if !RecognizedVideoFile(mediaPath) {
		return Series{}, fmt.Errorf("episode path %q is not a recognized video file", mediaPath)
	}
	companionPaths := make([]string, 0, len(opts.Companions))
	for _, companion := range opts.Companions {
		clean, err := cleanLibraryRelativePath(companion)
		if err != nil {
			return Series{}, err
		}
		companionPaths = append(companionPaths, clean)
	}

	seriesDir, err := findSeriesDir(root, mediaPath)
	if err != nil {
		return Series{}, err
	}
	seriesRelPath, err := filepath.Rel(seriesDir.Path(), root.Join(mediaPath))
	if err != nil {
		return Series{}, err
	}
	seriesRelPath = filepath.ToSlash(seriesRelPath)

	seriesCompanions := make([]string, 0, len(companionPaths))
	for _, companion := range companionPaths {
		seriesCompanion, err := filepath.Rel(seriesDir.Path(), root.Join(companion))
		if err != nil {
			return Series{}, err
		}
		seriesCompanions = append(seriesCompanions, filepath.ToSlash(seriesCompanion))
	}

	series, err := l.LoadSeries(seriesDir.Path())
	if err != nil {
		return Series{}, err
	}
	if opts.Inspector == nil {
		return Series{}, errors.New("library: media inspector is required")
	}
	progress.Start(ctx, "episode-import", fmt.Sprintf("Inspecting media: %s", mediaPath), 1)
	mediaInfo, err := opts.Inspector.Inspect(ctx, root.Join(mediaPath))
	if err != nil {
		progress.Failure(ctx, "episode-import", fmt.Sprintf("Failed inspecting %s", mediaPath), 1, 1)
		return Series{}, err
	}
	source := opts.Source
	if source == "" {
		source = InferSourceFromFilename(mediaPath)
	}
	progress.Update(ctx, "episode-import", fmt.Sprintf("Recording episode media: %s", mediaPath), 1, 1)
	updated, err := AddEpisode(seriesDir.Path(), *series, AddEpisodeOptions{
		Season:     opts.Season.Int(),
		Episode:    opts.Episode.Int(),
		Path:       seriesRelPath,
		Source:     source.String(),
		Companions: seriesCompanions,
		MediaInfo:  &mediaInfo,
	})
	if err != nil {
		progress.Failure(ctx, "episode-import", fmt.Sprintf("Failed recording %s", mediaPath), 1, 1)
		return Series{}, err
	}
	if opts.Apply {
		progress.Update(ctx, "episode-import", fmt.Sprintf("Writing series metadata: %s", SeriesPath(seriesDir.Path())), 1, 1)
		if err := l.SaveSeries(updated); err != nil {
			progress.Failure(ctx, "episode-import", "Failed writing series metadata", 1, 1)
			return Series{}, err
		}
	}
	progress.Success(ctx, "episode-import", fmt.Sprintf("Imported episode media: %s", mediaPath), 1)
	return updated, nil
}

func newSeriesFromProvider(lib Library, seriesDir string, providerSeries metadata.Series) (*Series, error) {
	series, err := lib.NewSeries(seriesDir)
	if err != nil {
		return nil, err
	}
	series.ProviderRefs = providerSeries.ProviderRefs
	if len(series.ProviderRefs) == 0 {
		series.ProviderRefs = []string{providerSeries.ProviderRef}
	}
	ref, err := metadata.ParseRemoteSeriesRef(providerSeries.ProviderRef)
	if err != nil {
		return nil, err
	}
	series.PreferredProvider = ref.Source()
	series.PreferredTitle = providerSeries.PreferredTitle
	series.CanonicalTitle = providerSeries.CanonicalTitle
	return series, nil
}

func unchangedTrackedEpisode(seriesDir SeriesDir, series Series, discovered DiscoveredEpisode) (Episode, bool, error) {
	season := Season{}
	if discovered.Season == 0 {
		if series.Specials == nil {
			return Episode{}, false, nil
		}
		season = *series.Specials
	} else {
		season = series.Seasons[strconv.Itoa(discovered.Season)]
	}
	episode, ok := season.Episodes[strconv.Itoa(discovered.Number)]
	if !ok || !CleanFilesystemTitle(episode.Media.Path).EqualName(discovered.Path) {
		return Episode{}, false, nil
	}

	info, err := os.Stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(discovered.Path)))
	if err != nil {
		return Episode{}, false, err
	}
	if info.IsDir() {
		return Episode{}, false, fmt.Errorf("series sync: media path %q is a directory", discovered.Path)
	}
	if episode.Media.Size != info.Size() {
		return Episode{}, false, nil
	}
	if episode.Media.MTime != info.ModTime().UTC().Format(time.RFC3339) {
		return Episode{}, false, nil
	}
	return episode, true, nil
}

func existingSyncEntry(discovered DiscoveredEpisode, episode Episode) SeriesSyncEntry {
	resolution := ""
	if episode.Media.MediaInfo != nil {
		resolution = episode.Media.MediaInfo.Resolution
	}
	companions := make([]string, 0, len(episode.Companions))
	for _, companion := range episode.Companions {
		companions = append(companions, companion.Path)
	}
	return SeriesSyncEntry{
		Status:     "existing",
		Season:     discovered.Season,
		Special:    discovered.Special,
		Number:     discovered.Number,
		Source:     ParseMediaSource(episode.Media.Source).Display(),
		Resolution: resolution,
		Path:       episode.Media.Path,
		Companions: companions,
	}
}

func cleanLibraryRelativePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path is required")
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("path %q must be relative to KURA_LIBRARY_ROOT", path)
	}
	slashPath := filepath.ToSlash(path)
	clean := filepath.Clean(filepath.FromSlash(slashPath))
	if clean == "." || clean == string(filepath.Separator) {
		return "", fmt.Errorf("path %q must point to a file", path)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes KURA_LIBRARY_ROOT", path)
	}
	return filepath.ToSlash(clean), nil
}

func findSeriesDir(root LibraryRoot, mediaPath string) (SeriesDir, error) {
	fullPath := root.Join(mediaPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		return SeriesDir{}, err
	}
	if info.IsDir() {
		return SeriesDir{}, fmt.Errorf("episode path %q is a directory", mediaPath)
	}

	dir := filepath.Dir(fullPath)
	for {
		if _, err := os.Stat(SeriesPath(dir)); err == nil {
			return ParseSeriesDir(dir)
		} else if !errors.Is(err, os.ErrNotExist) {
			return SeriesDir{}, err
		}
		if dir == root.Path() {
			return SeriesDir{}, fmt.Errorf("could not find %s above %q", SeriesPath("<series>"), mediaPath)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return SeriesDir{}, fmt.Errorf("could not find series metadata above %q", mediaPath)
		}
		dir = parent
	}
}

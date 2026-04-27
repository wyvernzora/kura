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

type MediaInspectorFunc func(context.Context, string) (MediaInfo, error)

func (f MediaInspectorFunc) Inspect(ctx context.Context, path string) (MediaInfo, error) {
	return f(ctx, path)
}

type ProviderSeriesResolver func(context.Context, Series) (metadata.Series, error)

type SeriesSyncOptions struct {
	ProviderSeries   *metadata.Series
	ProviderResolver ProviderSeriesResolver
	Inspector        MediaInspector
	Apply            bool
	DryRun           bool
	Replace          bool
}

type SeriesSyncResult struct {
	Series        string            `json:"series"`
	Initialized   bool              `json:"initialized"`
	DryRun        bool              `json:"dryRun"`
	Synced        []SeriesSyncEntry `json:"synced"`
	Skipped       []ImportSkip      `json:"skipped"`
	UpdatedSeries Series            `json:"-"`
	UpdatedTrash  Trash             `json:"-"`
}

func (r SeriesSyncResult) HasChanges() bool {
	if r.Initialized {
		return true
	}
	for _, entry := range r.Synced {
		if entry.Status != "existing" {
			return true
		}
	}
	return false
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
		initialized = true
	} else {
		return SeriesSyncResult{}, err
	}

	discovered, skipped, err := DiscoverSeriesEpisodes(seriesDir)
	if err != nil {
		return SeriesSyncResult{}, err
	}
	if err := validateUniqueDiscoveredEpisodes(discovered); err != nil {
		return SeriesSyncResult{}, err
	}

	updated := *series
	trash, err := l.store.LoadTrash(seriesDir.Path())
	if err != nil {
		return SeriesSyncResult{}, err
	}
	updatedTrash := *trash
	var providerSeries *metadata.Series
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
		if providerSeries == nil {
			providerSeries, err = providerSeriesForLocal(ctx, updated, opts.ProviderSeries, opts.ProviderResolver)
			if err != nil {
				return SeriesSyncResult{}, err
			}
		}
		if err := validateProviderEpisode(providerSeries, episode.Season, episode.Number); err != nil {
			return SeriesSyncResult{}, err
		}
		replacing := episodeExists(updated, episode.Season, episode.Number)
		if replacing && !opts.Replace {
			return SeriesSyncResult{}, EpisodeAlreadyExistsError{Season: episode.Season, Episode: episode.Number}
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
			Replace:    opts.Replace,
			Trash:      &updatedTrash,
		})
		if err != nil {
			progress.Failure(ctx, "series-sync", fmt.Sprintf("Failed recording %s", episode.Path), index+1, len(discovered))
			return SeriesSyncResult{}, err
		}
		updated = next
		status := "new"
		if replacing {
			status = "replaced"
		}
		synced = append(synced, SeriesSyncEntry{
			Status:     status,
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
		UpdatedTrash:  updatedTrash,
	}
	if opts.Apply && result.HasChanges() {
		progress.Start(ctx, "series-sync-write", fmt.Sprintf("Writing series metadata: %s", SeriesPath(seriesDir.Path())), 0)
		if err := l.SaveSeries(updated); err != nil {
			progress.Failure(ctx, "series-sync-write", "Failed writing series metadata", 0, 0)
			return SeriesSyncResult{}, err
		}
		if err := l.SaveTrash(updatedTrash); err != nil {
			progress.Failure(ctx, "series-sync-write", "Failed writing trash metadata", 0, 0)
			return SeriesSyncResult{}, err
		}
		progress.Success(ctx, "series-sync-write", fmt.Sprintf("Synced %d episode media file(s)", len(synced)), len(synced))
	}
	return result, nil
}

type ImportEpisodeFileOptions struct {
	Season           SeasonNumber
	Episode          EpisodeNumber
	Source           MediaSource
	Companions       []string
	MediaPath        string
	Inspector        MediaInspector
	ProviderSeries   *metadata.Series
	ProviderResolver ProviderSeriesResolver
	Apply            bool
	Replace          bool
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
	trash, err := l.store.LoadTrash(seriesDir.Path())
	if err != nil {
		return Series{}, err
	}
	providerSeries, err := providerSeriesForLocal(ctx, *series, opts.ProviderSeries, opts.ProviderResolver)
	if err != nil {
		return Series{}, err
	}
	if err := validateProviderEpisode(providerSeries, opts.Season.Int(), opts.Episode.Int()); err != nil {
		return Series{}, err
	}
	if episodeExists(*series, opts.Season.Int(), opts.Episode.Int()) && !opts.Replace {
		return Series{}, EpisodeAlreadyExistsError{Season: opts.Season.Int(), Episode: opts.Episode.Int()}
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
		Replace:    opts.Replace,
		Trash:      trash,
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
		if err := l.SaveTrash(*trash); err != nil {
			progress.Failure(ctx, "episode-import", "Failed writing trash metadata", 1, 1)
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

func providerSeriesForLocal(ctx context.Context, series Series, explicit *metadata.Series, resolve ProviderSeriesResolver) (*metadata.Series, error) {
	if explicit != nil {
		return explicit, nil
	}
	if resolve == nil {
		return nil, nil
	}
	providerSeries, err := resolve(ctx, series)
	if err != nil {
		return nil, err
	}
	return &providerSeries, nil
}

func validateProviderEpisode(providerSeries *metadata.Series, seasonNumber int, episodeNumber int) error {
	if providerSeries == nil {
		return errors.New("library: provider series metadata is required to import episodes")
	}
	if providerEpisodeExists(*providerSeries, seasonNumber, episodeNumber) {
		return nil
	}
	return fmt.Errorf("library: provider metadata has no S%02dE%02d", seasonNumber, episodeNumber)
}

func providerEpisodeExists(series metadata.Series, seasonNumber int, episodeNumber int) bool {
	if seasonNumber == 0 {
		for _, episode := range series.Specials {
			if episode.SeasonNumber == 0 && episode.EpisodeNumber == episodeNumber {
				return true
			}
		}
		return false
	}
	for _, season := range series.Seasons {
		if season.Number != seasonNumber {
			continue
		}
		for _, episode := range season.Episodes {
			if episode.EpisodeNumber == episodeNumber {
				return true
			}
		}
		return false
	}
	return false
}

func validateUniqueDiscoveredEpisodes(discovered []DiscoveredEpisode) error {
	seen := map[string]DiscoveredEpisode{}
	for _, episode := range discovered {
		key := episodeKey(episode.Season, episode.Number)
		existing, exists := seen[key]
		if exists {
			return fmt.Errorf("series sync: multiple files parsed as S%02dE%02d: %s and %s", episode.Season, episode.Number, existing.Path, episode.Path)
		}
		seen[key] = episode
	}
	return nil
}

func episodeExists(series Series, seasonNumber int, episodeNumber int) bool {
	_, ok := series.LookupEpisode(seasonNumber, episodeNumber)
	return ok
}

func episodeKey(seasonNumber int, episodeNumber int) string {
	return strconv.Itoa(seasonNumber) + ":" + strconv.Itoa(episodeNumber)
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

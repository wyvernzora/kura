package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/store"
)

type SeriesSyncOptions struct {
	ProviderSeries   *metadata.Series
	ProviderResolver ProviderSeriesResolver
	Inspector        MediaInspector
	Apply            bool
	DryRun           bool
	Replace          bool
}

type SeriesSyncResult struct {
	Series        string              `json:"series"`
	Initialized   bool                `json:"initialized"`
	DryRun        bool                `json:"dryRun"`
	Synced        []SeriesSyncEntry   `json:"synced"`
	Skipped       []fsroot.ImportSkip `json:"skipped"`
	UpdatedSeries store.Series        `json:"-"`
	UpdatedTrash  store.Trash         `json:"-"`
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

func SyncSeries(ctx context.Context, repo store.Repo, root fsroot.LibraryRoot, dirname string, opts SeriesSyncOptions) (SeriesSyncResult, error) {
	seriesDir, err := root.SeriesDir(dirname)
	if err != nil {
		return SeriesSyncResult{}, err
	}

	var initialized bool
	var series *store.Series
	if _, err := os.Stat(store.SeriesPath(seriesDir.Path())); err == nil {
		series, err = repo.LoadSeries(seriesDir.Path())
		if err != nil {
			return SeriesSyncResult{}, err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if opts.ProviderSeries == nil {
			return SeriesSyncResult{}, fmt.Errorf("library: provider series is required to initialize %q", dirname)
		}
		series, err = newSeriesFromProvider(repo, seriesDir.Path(), *opts.ProviderSeries)
		if err != nil {
			return SeriesSyncResult{}, err
		}
		initialized = true
	} else {
		return SeriesSyncResult{}, err
	}

	discovered, skipped, err := fsroot.DiscoverSeriesEpisodes(seriesDir)
	if err != nil {
		return SeriesSyncResult{}, err
	}
	if err := validateUniqueDiscoveredEpisodes(discovered); err != nil {
		return SeriesSyncResult{}, err
	}

	updated := *series
	trash, err := repo.LoadTrash(seriesDir.Path())
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
		trackedEpisode, tracked := updated.LookupEpisode(episode.Season, episode.Number)
		refreshing := tracked && domain.CleanFilesystemTitle(trackedEpisode.Media.Path).EqualName(episode.Path)
		replacing := tracked && !refreshing
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
			Replace:    opts.Replace && replacing,
			Refresh:    refreshing,
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
		} else if refreshing {
			status = "updated"
		}
		synced = append(synced, SeriesSyncEntry{
			Status:     status,
			Season:     episode.Season,
			Special:    episode.Special,
			Number:     episode.Number,
			Source:     domain.ParseMediaSource(episode.Source).Display(),
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
	if opts.Apply && !opts.DryRun && result.HasChanges() {
		progress.Start(ctx, "series-sync-write", fmt.Sprintf("Writing series metadata: %s", store.SeriesPath(seriesDir.Path())), 0)
		if err := repo.SaveSeries(updated); err != nil {
			progress.Failure(ctx, "series-sync-write", "Failed writing series metadata", 0, 0)
			return SeriesSyncResult{}, err
		}
		if err := repo.SaveTrash(updatedTrash); err != nil {
			progress.Failure(ctx, "series-sync-write", "Failed writing trash metadata", 0, 0)
			return SeriesSyncResult{}, err
		}
		progress.Success(ctx, "series-sync-write", fmt.Sprintf("Synced %d episode media file(s)", len(synced)), len(synced))
	}
	return result, nil
}

func newSeriesFromProvider(repo store.Repo, seriesDir string, providerSeries metadata.Series) (*store.Series, error) {
	series, err := repo.NewSeries(seriesDir)
	if err != nil {
		return nil, err
	}
	series.ProviderRefs = providerSeries.ProviderRefs
	if len(series.ProviderRefs) == 0 {
		series.ProviderRefs = []string{providerSeries.ProviderRef}
	}
	ref, err := domain.ParseRemoteSeriesRef(providerSeries.ProviderRef)
	if err != nil {
		return nil, err
	}
	series.PreferredProvider = ref.Source()
	series.PreferredTitle = providerSeries.PreferredTitle
	series.CanonicalTitle = providerSeries.CanonicalTitle
	return series, nil
}

func providerSeriesForLocal(ctx context.Context, series store.Series, explicit *metadata.Series, resolve ProviderSeriesResolver) (*metadata.Series, error) {
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
		if series.Specials == nil {
			return false
		}
		for _, episode := range series.Specials.Episodes {
			if episode.EpisodeNumber == episodeNumber {
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

func validateUniqueDiscoveredEpisodes(discovered []fsroot.DiscoveredEpisode) error {
	seen := map[string]fsroot.DiscoveredEpisode{}
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

func episodeExists(series store.Series, seasonNumber int, episodeNumber int) bool {
	_, ok := series.LookupEpisode(seasonNumber, episodeNumber)
	return ok
}

func episodeKey(seasonNumber int, episodeNumber int) string {
	return strconv.Itoa(seasonNumber) + ":" + strconv.Itoa(episodeNumber)
}

func unchangedTrackedEpisode(seriesDir fsroot.SeriesDir, series store.Series, discovered fsroot.DiscoveredEpisode) (store.Episode, bool, error) {
	episode, ok := series.LookupEpisode(discovered.Season, discovered.Number)
	if !ok || !domain.CleanFilesystemTitle(episode.Media.Path).EqualName(discovered.Path) {
		return store.Episode{}, false, nil
	}

	info, err := os.Stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(discovered.Path)))
	if err != nil {
		return store.Episode{}, false, err
	}
	if info.IsDir() {
		return store.Episode{}, false, fmt.Errorf("series sync: media path %q is a directory", discovered.Path)
	}
	if episode.Media.Size != info.Size() {
		return store.Episode{}, false, nil
	}
	if episode.Media.MTime != info.ModTime().UTC().Format(time.RFC3339) {
		return store.Episode{}, false, nil
	}
	if !companionsUnchanged(seriesDir, episode.Companions, discovered.Companions) {
		return store.Episode{}, false, nil
	}
	return episode, true, nil
}

func companionsUnchanged(seriesDir fsroot.SeriesDir, tracked []store.CompanionFile, discovered []string) bool {
	if len(tracked) != len(discovered) {
		return false
	}
	trackedByPath := make(map[string]store.CompanionFile, len(tracked))
	for _, companion := range tracked {
		trackedByPath[companion.Path] = companion
	}
	for _, path := range discovered {
		companion, ok := trackedByPath[path]
		if !ok {
			return false
		}
		info, err := os.Stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(path)))
		if err != nil || info.IsDir() {
			return false
		}
		if companion.Size != info.Size() {
			return false
		}
		if companion.MTime != info.ModTime().UTC().Format(time.RFC3339) {
			return false
		}
	}
	return true
}

func existingSyncEntry(discovered fsroot.DiscoveredEpisode, episode store.Episode) SeriesSyncEntry {
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
		Source:     domain.ParseMediaSource(episode.Media.Source).Display(),
		Resolution: resolution,
		Path:       episode.Media.Path,
		Companions: companions,
	}
}

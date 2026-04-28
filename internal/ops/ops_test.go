package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/store"
)

func TestSyncSeriesInitializesAndImportsSeasonEpisodes(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv"), "episode 1")
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).en.ass"), "subtitle")
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E02.mkv"), "episode 2")
	writeFile(t, filepath.Join(seasonDir, "Bookworm - bonus.mkv"), "bonus")

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	repo := store.NewRepo()
	providerSeries := testProviderSeries()
	result, err := SyncSeries(context.Background(), repo, root, "Bookworm", SeriesSyncOptions{
		ProviderSeries: &providerSeries,
		Inspector:      fakeInspector,
		Apply:          true,
	})
	if err != nil {
		t.Fatalf("SyncSeries: %v", err)
	}
	if !result.Initialized {
		t.Fatal("Initialized = false, want true")
	}
	if len(result.Synced) != 2 {
		t.Fatalf("len(Synced) = %d, want 2", len(result.Synced))
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Reason != "could not infer episode number" {
		t.Fatalf("Skipped = %#v, want bonus skip", result.Skipped)
	}

	loaded, err := repo.LoadSeries(seriesDir)
	if err != nil {
		t.Fatalf("LoadSeries: %v", err)
	}
	episode := testEpisode(t, loaded, 1, 1)
	if episode.Media.Path != "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv" {
		t.Fatalf("Media.Path = %q", episode.Media.Path)
	}
	if len(episode.Companions) != 1 {
		t.Fatalf("len(Companions) = %d, want 1", len(episode.Companions))
	}
}

func TestSyncSeriesInitializesEmptyDirectory(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll series: %v", err)
	}

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	repo := store.NewRepo()
	providerSeries := testProviderSeries()
	result, err := SyncSeries(context.Background(), repo, root, "Bookworm", SeriesSyncOptions{
		ProviderSeries: &providerSeries,
		Apply:          true,
	})
	if err != nil {
		t.Fatalf("SyncSeries: %v", err)
	}
	if !result.Initialized {
		t.Fatal("Initialized = false, want true")
	}
	if len(result.Synced) != 0 || len(result.Skipped) != 0 {
		t.Fatalf("Synced/Skipped = %#v/%#v, want empty", result.Synced, result.Skipped)
	}
	loaded, err := repo.LoadSeries(seriesDir)
	if err != nil {
		t.Fatalf("LoadSeries: %v", err)
	}
	if loaded.PreferredTitle != providerSeries.PreferredTitle {
		t.Fatalf("PreferredTitle = %q, want %q", loaded.PreferredTitle, providerSeries.PreferredTitle)
	}
}

func TestSyncSeriesDoesNotPersistFilesystemTitle(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Short Title")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "Short Title - S01E01.mkv"), "episode")

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	repo := store.NewRepo()
	providerSeries := testProviderSeries()
	providerSeries.PreferredTitle = "A Much Longer Provider Title"
	providerSeries.CanonicalTitle = "Canonical Provider Title"
	if _, err := SyncSeries(context.Background(), repo, root, "Short Title", SeriesSyncOptions{
		ProviderSeries: &providerSeries,
		Inspector:      fakeInspector,
		Apply:          true,
	}); err != nil {
		t.Fatalf("SyncSeries: %v", err)
	}

	loaded, err := repo.LoadSeries(seriesDir)
	if err != nil {
		t.Fatalf("LoadSeries: %v", err)
	}
	data, err := json.Marshal(loaded)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := raw["filesystemTitle"]; ok {
		t.Fatal("filesystemTitle present, want derived from directory name")
	}
}

func TestSyncSeriesKeepsUnchangedTrackedEpisodeWithoutInspector(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll .kura: %v", err)
	}
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	episodePath := filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv")
	writeFile(t, episodePath, "episode")
	info, err := os.Stat(episodePath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	writeSeriesJSON(t, seriesDir, fmt.Sprintf(`{
		"schemaVersion": 1,
		"id": "01JZ7P0Q2V3W4X5Y6Z7A8B9C0D",
		"providerRefs": ["tvdb:370070"],
		"preferredProvider": "tvdb",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm",
		"seasons": [
			{
				"number": 1,
				"episodes": [
					{
						"number": 1,
						"media": {
							"path": "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv",
							"source": "webrip",
							"size": %d,
							"mtime": %q,
							"mediainfo": {"videoCodec": "HEVC", "resolution": "1920x1080"}
						},
						"companions": []
					}
				]
			}
		]
	}`, info.Size(), info.ModTime().UTC().Format(time.RFC3339)))

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	result, err := SyncSeries(context.Background(), store.NewRepo(), root, "Bookworm", SeriesSyncOptions{})
	if err != nil {
		t.Fatalf("SyncSeries: %v", err)
	}
	if len(result.Synced) != 1 || result.Synced[0].Status != "existing" {
		t.Fatalf("Synced = %#v, want existing entry", result.Synced)
	}
	if result.HasChanges() {
		t.Fatal("HasChanges = true, want false")
	}
}

func TestSyncSeriesRefreshesChangedCompanionWithoutReplace(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll .kura: %v", err)
	}
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	episodePath := filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv")
	companionPath := filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).en.ass")
	writeFile(t, episodePath, "episode")
	writeFile(t, companionPath, "updated subtitle")
	episodeInfo, err := os.Stat(episodePath)
	if err != nil {
		t.Fatalf("Stat episode: %v", err)
	}
	writeSeriesJSON(t, seriesDir, fmt.Sprintf(`{
		"schemaVersion": 1,
		"id": "01JZ7P0Q2V3W4X5Y6Z7A8B9C0D",
		"providerRefs": ["tvdb:370070"],
		"preferredProvider": "tvdb",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm",
		"seasons": [
			{
				"number": 1,
				"episodes": [
					{
						"number": 1,
						"media": {
							"path": "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv",
							"source": "webrip",
							"size": %d,
							"mtime": %q,
							"mediainfo": {"videoCodec": "HEVC", "resolution": "1920x1080"}
						},
						"companions": [
							{"path": "Season 1/Bookworm - S01E01 (WebRip 1080p).en.ass", "size": 1, "mtime": "2026-04-20T03:00:00Z"}
						]
					}
				]
			}
		]
	}`, episodeInfo.Size(), episodeInfo.ModTime().UTC().Format(time.RFC3339)))

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	repo := store.NewRepo()
	providerSeries := testProviderSeries()
	result, err := SyncSeries(context.Background(), repo, root, "Bookworm", SeriesSyncOptions{
		ProviderSeries: &providerSeries,
		Inspector:      fakeInspector,
		Apply:          true,
	})
	if err != nil {
		t.Fatalf("SyncSeries: %v", err)
	}
	if len(result.Synced) != 1 || result.Synced[0].Status != "updated" {
		t.Fatalf("Synced = %#v, want updated entry", result.Synced)
	}
	loaded, err := repo.LoadSeries(seriesDir)
	if err != nil {
		t.Fatalf("LoadSeries: %v", err)
	}
	companionInfo, err := os.Stat(companionPath)
	if err != nil {
		t.Fatalf("Stat companion: %v", err)
	}
	got := testEpisode(t, loaded, 1, 1).Companions[0]
	if got.Size != companionInfo.Size() || got.MTime != companionInfo.ModTime().UTC().Format(time.RFC3339) {
		t.Fatalf("Companion facts = %#v, want refreshed from filesystem", got)
	}
}

func TestSyncSeriesDryRunDoesNotApplyWhenApplyIsAlsoTrue(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01.mkv"), "episode")

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	providerSeries := testProviderSeries()
	result, err := SyncSeries(context.Background(), store.NewRepo(), root, "Bookworm", SeriesSyncOptions{
		ProviderSeries: &providerSeries,
		Inspector:      fakeInspector,
		Apply:          true,
		DryRun:         true,
	})
	if err != nil {
		t.Fatalf("SyncSeries: %v", err)
	}
	if !result.HasChanges() {
		t.Fatal("HasChanges = false, want true")
	}
	if _, err := os.Stat(store.SeriesPath(seriesDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat series.json = %v, want not exist", err)
	}
}

func TestSyncSeriesApplySkipsUnchangedMetadata(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll .kura: %v", err)
	}
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	episodePath := filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv")
	writeFile(t, episodePath, "episode")
	info, err := os.Stat(episodePath)
	if err != nil {
		t.Fatalf("Stat episode: %v", err)
	}
	writeSeriesJSON(t, seriesDir, fmt.Sprintf(`{
		"schemaVersion": 1,
		"id": "01JZ7P0Q2V3W4X5Y6Z7A8B9C0D",
		"providerRefs": ["tvdb:370070"],
		"preferredProvider": "tvdb",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm",
		"seasons": [
			{
				"number": 1,
				"episodes": [
					{
						"number": 1,
						"media": {
							"path": "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv",
							"source": "webrip",
							"size": %d,
							"mtime": %q,
							"mediainfo": {"videoCodec": "HEVC", "resolution": "1920x1080"}
						},
						"companions": []
					}
				]
			}
		]
	}`, info.Size(), info.ModTime().UTC().Format(time.RFC3339)))

	metadataPath := store.SeriesPath(seriesDir)
	originalTime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(metadataPath, originalTime, originalTime); err != nil {
		t.Fatalf("Chtimes series.json: %v", err)
	}
	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}

	result, err := SyncSeries(context.Background(), store.NewRepo(), root, "Bookworm", SeriesSyncOptions{Apply: true})
	if err != nil {
		t.Fatalf("SyncSeries: %v", err)
	}
	if result.HasChanges() {
		t.Fatal("HasChanges = true, want false")
	}
	metadataInfo, err := os.Stat(metadataPath)
	if err != nil {
		t.Fatalf("Stat series.json: %v", err)
	}
	if !metadataInfo.ModTime().Equal(originalTime) {
		t.Fatalf("series.json mtime = %s, want %s", metadataInfo.ModTime(), originalTime)
	}
}

func TestSyncSeriesRejectsEpisodeMissingFromProviderMetadata(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E03.mkv"), "episode 3")

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	providerSeries := testProviderSeries()
	_, err = SyncSeries(context.Background(), store.NewRepo(), root, "Bookworm", SeriesSyncOptions{
		ProviderSeries: &providerSeries,
		Inspector:      fakeInspector,
	})
	if err == nil || !strings.Contains(err.Error(), "provider metadata has no S01E03") {
		t.Fatalf("SyncSeries error = %v, want missing provider episode", err)
	}
}

func TestSyncSeriesRejectsDuplicateParsedEpisodes(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01.mkv"), "episode 1")
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (alt).mkv"), "episode 1 alt")

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	providerSeries := testProviderSeries()
	_, err = SyncSeries(context.Background(), store.NewRepo(), root, "Bookworm", SeriesSyncOptions{
		ProviderSeries: &providerSeries,
		Inspector:      fakeInspector,
	})
	if err == nil || !strings.Contains(err.Error(), "multiple files parsed as S01E01") {
		t.Fatalf("SyncSeries error = %v, want duplicate parsed episode", err)
	}
}

func TestStageEpisodeFileRecordsAbsolutePathsInStaged(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll series: %v", err)
	}
	repo := store.NewRepo()
	series, err := newTestSeries(repo, seriesDir)
	if err != nil {
		t.Fatalf("testSeries: %v", err)
	}
	if err := repo.SaveSeries(*series); err != nil {
		t.Fatalf("SaveSeries: %v", err)
	}
	stageDir := t.TempDir()
	mediaPath := filepath.Join(stageDir, "Bookworm - S01E01 (WebRip).mkv")
	companionPath := filepath.Join(stageDir, "Bookworm - S01E01 (WebRip).en.ass")
	writeFile(t, mediaPath, "episode")
	writeFile(t, companionPath, "subtitle")

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	season, _ := domain.RegularSeason(1)
	episode, _ := domain.NewEpisodeNumber(1)
	providerSeries := testProviderSeries()
	result, err := StageEpisodeFile(context.Background(), repo, root, "Bookworm", StageEpisodeFileOptions{
		Season:         season,
		Episode:        episode,
		Companions:     []string{companionPath},
		MediaPath:      mediaPath,
		Inspector:      fakeInspector,
		ProviderSeries: &providerSeries,
		Apply:          true,
	})
	if err != nil {
		t.Fatalf("StageEpisodeFile: %v", err)
	}
	if result.Entry.Media.Path != mediaPath {
		t.Fatalf("Media.Path = %q, want %q", result.Entry.Media.Path, mediaPath)
	}
	staged, err := repo.LoadStaged(seriesDir)
	if err != nil {
		t.Fatalf("LoadStaged: %v", err)
	}
	if len(staged.Entries) != 1 {
		t.Fatalf("len(Staged.Entries) = %d, want 1", len(staged.Entries))
	}
	if staged.Entries[0].Media.Path != mediaPath {
		t.Fatalf("Staged media path = %q, want %q", staged.Entries[0].Media.Path, mediaPath)
	}
	if len(staged.Entries[0].Companions) != 1 || staged.Entries[0].Companions[0].Path != companionPath {
		t.Fatalf("Staged companions = %#v", staged.Entries[0].Companions)
	}
}

func TestStageEpisodeFileReplaceOverwritesStagedEntry(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll series: %v", err)
	}
	repo := store.NewRepo()
	series, err := newTestSeries(repo, seriesDir)
	if err != nil {
		t.Fatalf("testSeries: %v", err)
	}
	if err := repo.SaveSeries(*series); err != nil {
		t.Fatalf("SaveSeries: %v", err)
	}
	stageDir := t.TempDir()
	firstPath := filepath.Join(stageDir, "first.mkv")
	secondPath := filepath.Join(stageDir, "second.mkv")
	writeFile(t, firstPath, "first")
	writeFile(t, secondPath, "second")

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	season, _ := domain.RegularSeason(1)
	episode, _ := domain.NewEpisodeNumber(1)
	providerSeries := testProviderSeries()
	if _, err := StageEpisodeFile(context.Background(), repo, root, "Bookworm", StageEpisodeFileOptions{
		Season:         season,
		Episode:        episode,
		MediaPath:      firstPath,
		Inspector:      fakeInspector,
		ProviderSeries: &providerSeries,
		Apply:          true,
	}); err != nil {
		t.Fatalf("StageEpisodeFile first: %v", err)
	}
	if _, err := StageEpisodeFile(context.Background(), repo, root, "Bookworm", StageEpisodeFileOptions{
		Season:         season,
		Episode:        episode,
		MediaPath:      secondPath,
		Inspector:      fakeInspector,
		ProviderSeries: &providerSeries,
		Apply:          true,
	}); err == nil {
		t.Fatal("StageEpisodeFile second returned nil error, want staged episode exists error")
	}
	result, err := StageEpisodeFile(context.Background(), repo, root, "Bookworm", StageEpisodeFileOptions{
		Season:         season,
		Episode:        episode,
		MediaPath:      secondPath,
		Inspector:      fakeInspector,
		ProviderSeries: &providerSeries,
		Apply:          true,
		Replace:        true,
	})
	if err != nil {
		t.Fatalf("StageEpisodeFile replace: %v", err)
	}
	if !result.Replaced {
		t.Fatal("Replaced = false, want true")
	}
	staged, err := repo.LoadStaged(seriesDir)
	if err != nil {
		t.Fatalf("LoadStaged: %v", err)
	}
	if len(staged.Entries) != 1 || staged.Entries[0].Media.Path != secondPath {
		t.Fatalf("Staged entries = %#v, want single replacement", staged.Entries)
	}
}

func TestStageEpisodeFileRequiresReplaceForActiveEpisode(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll .kura: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "active.mkv"), "active")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"id": "01JZ7P0Q2V3W4X5Y6Z7A8B9C0D",
		"providerRefs": ["tvdb:370070"],
		"preferredProvider": "tvdb",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm",
		"seasons": [
			{
				"number": 1,
				"episodes": [
					{
						"number": 1,
						"media": {
							"path": "Season 1/active.mkv",
							"source": "webrip",
							"size": 6,
							"mtime": "2026-04-20T03:00:00Z"
						},
						"companions": []
					}
				]
			}
		]
	}`)
	stageDir := t.TempDir()
	mediaPath := filepath.Join(stageDir, "replacement.mkv")
	writeFile(t, mediaPath, "replacement")

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	season, _ := domain.RegularSeason(1)
	episode, _ := domain.NewEpisodeNumber(1)
	providerSeries := testProviderSeries()
	repo := store.NewRepo()
	if _, err := StageEpisodeFile(context.Background(), repo, root, "Bookworm", StageEpisodeFileOptions{
		Season:         season,
		Episode:        episode,
		MediaPath:      mediaPath,
		Inspector:      fakeInspector,
		ProviderSeries: &providerSeries,
		Apply:          true,
	}); err == nil {
		t.Fatal("StageEpisodeFile returned nil error, want active episode exists error")
	}
	result, err := StageEpisodeFile(context.Background(), repo, root, "Bookworm", StageEpisodeFileOptions{
		Season:         season,
		Episode:        episode,
		MediaPath:      mediaPath,
		Inspector:      fakeInspector,
		ProviderSeries: &providerSeries,
		Apply:          true,
		Replace:        true,
	})
	if err != nil {
		t.Fatalf("StageEpisodeFile replace: %v", err)
	}
	if !result.Replaced {
		t.Fatal("Replaced = false, want true")
	}
	staged, err := repo.LoadStaged(seriesDir)
	if err != nil {
		t.Fatalf("LoadStaged: %v", err)
	}
	if len(staged.Entries) != 1 || staged.Entries[0].Media.Path != mediaPath {
		t.Fatalf("Staged entries = %#v, want staged replacement", staged.Entries)
	}
}

func TestReconcileAppliesStagedEpisode(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll series: %v", err)
	}
	repo := store.NewRepo()
	series, err := newTestSeries(repo, seriesDir)
	if err != nil {
		t.Fatalf("testSeries: %v", err)
	}
	if err := repo.SaveSeries(*series); err != nil {
		t.Fatalf("SaveSeries: %v", err)
	}

	stageDir := t.TempDir()
	mediaPath := filepath.Join(stageDir, "incoming.mkv")
	companionPath := filepath.Join(stageDir, "incoming.en.ass")
	writeFile(t, mediaPath, "episode")
	writeFile(t, companionPath, "subtitle")
	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	season, _ := domain.RegularSeason(1)
	episode, _ := domain.NewEpisodeNumber(1)
	providerSeries := testProviderSeries()
	if _, err := StageEpisodeFile(context.Background(), repo, root, "Bookworm", StageEpisodeFileOptions{
		Season:         season,
		Episode:        episode,
		Source:         domain.MediaSourceWebRip,
		Companions:     []string{companionPath},
		MediaPath:      mediaPath,
		Inspector:      fakeInspector,
		ProviderSeries: &providerSeries,
		Apply:          true,
	}); err != nil {
		t.Fatalf("StageEpisodeFile: %v", err)
	}

	plan, err := PlanSeries(context.Background(), root, "Bookworm", repo)
	if err != nil {
		t.Fatalf("PlanSeries: %v", err)
	}
	if len(plan.FileMoves) != 2 {
		t.Fatalf("len(FileMoves) = %d, want media plus companion", len(plan.FileMoves))
	}
	if err := ApplyPlan(context.Background(), plan, repo); err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}

	targetMediaPath := filepath.Join(seriesDir, "Season 1", "Bookworm - S01E01 (WebRip 1080p).mkv")
	targetCompanionPath := filepath.Join(seriesDir, "Season 1", "Bookworm - S01E01 (WebRip 1080p).en.ass")
	for _, path := range []string{targetMediaPath, targetCompanionPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Stat %s: %v", path, err)
		}
	}
	if _, err := os.Stat(mediaPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat staged media = %v, want not exist", err)
	}
	if _, err := os.Stat(store.StagedPath(seriesDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat staged.json = %v, want not exist", err)
	}
	loaded, err := repo.LoadSeries(seriesDir)
	if err != nil {
		t.Fatalf("LoadSeries: %v", err)
	}
	if got := testEpisode(t, loaded, 1, 1).Media.Path; got != "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv" {
		t.Fatalf("Media.Path = %q, want reconciled path", got)
	}
	if got := testEpisode(t, loaded, 1, 1).Companions[0].Path; got != "Season 1/Bookworm - S01E01 (WebRip 1080p).en.ass" {
		t.Fatalf("Companion.Path = %q, want reconciled path", got)
	}
}

func TestReconcileAppliesStagedReplacementAfterTrashingActiveEpisode(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll .kura: %v", err)
	}
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	activeRelPath := "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv"
	writeFile(t, filepath.Join(seriesDir, filepath.FromSlash(activeRelPath)), "old episode")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"id": "01JZ7P0Q2V3W4X5Y6Z7A8B9C0D",
		"providerRefs": ["tvdb:370070"],
		"preferredProvider": "tvdb",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm",
		"seasons": [
			{
				"number": 1,
				"episodes": [
					{
						"number": 1,
						"media": {
							"path": "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv",
							"source": "webrip",
							"size": 11,
							"mtime": "2026-04-20T03:00:00Z",
							"mediainfo": {"videoCodec": "HEVC", "resolution": "1920x1080"}
						},
						"companions": []
					}
				]
			}
		]
	}`)
	stageDir := t.TempDir()
	mediaPath := filepath.Join(stageDir, "replacement.mkv")
	writeFile(t, mediaPath, "new episode")

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	season, _ := domain.RegularSeason(1)
	episode, _ := domain.NewEpisodeNumber(1)
	providerSeries := testProviderSeries()
	repo := store.NewRepo()
	if _, err := StageEpisodeFile(context.Background(), repo, root, "Bookworm", StageEpisodeFileOptions{
		Season:         season,
		Episode:        episode,
		Source:         domain.MediaSourceWebRip,
		MediaPath:      mediaPath,
		Inspector:      fakeInspector,
		ProviderSeries: &providerSeries,
		Apply:          true,
		Replace:        true,
	}); err != nil {
		t.Fatalf("StageEpisodeFile replace: %v", err)
	}
	plan, err := PlanSeries(context.Background(), root, "Bookworm", repo)
	if err != nil {
		t.Fatalf("PlanSeries: %v", err)
	}
	if len(plan.FileMoves) != 2 {
		t.Fatalf("len(FileMoves) = %d, want trash move plus staged media move", len(plan.FileMoves))
	}
	if err := ApplyPlan(context.Background(), plan, repo); err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}

	trash, err := repo.LoadTrash(seriesDir)
	if err != nil {
		t.Fatalf("LoadTrash: %v", err)
	}
	if len(trash.Entries) != 1 {
		t.Fatalf("len(Trash.Entries) = %d, want 1", len(trash.Entries))
	}
	if _, err := os.Stat(filepath.Join(seriesDir, ".kura", "trash", trash.Entries[0].ID, "Bookworm - S01E01 (WebRip 1080p).mkv")); err != nil {
		t.Fatalf("Stat trashed active episode: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(seriesDir, filepath.FromSlash(activeRelPath)))
	if err != nil {
		t.Fatalf("ReadFile active replacement: %v", err)
	}
	if string(data) != "new episode" {
		t.Fatalf("active replacement content = %q, want new episode", data)
	}
	if _, err := os.Stat(store.StagedPath(seriesDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat staged.json = %v, want not exist", err)
	}
}

func TestSyncSeriesReplaceMovesExistingEpisodeToTrashDuringReconcile(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll .kura: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "old.mkv"), "old episode")
	writeFile(t, filepath.Join(seasonDir, "old.en.ass"), "old subtitle")
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip).mkv"), "new episode")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"id": "01JZ7P0Q2V3W4X5Y6Z7A8B9C0D",
		"providerRefs": ["tvdb:370070"],
		"preferredProvider": "tvdb",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm",
		"seasons": [
			{
				"number": 1,
				"episodes": [
					{
						"number": 1,
						"media": {
							"path": "Season 1/old.mkv",
							"source": "webrip",
							"size": 11,
							"mtime": "2026-04-20T03:00:00Z"
						},
						"companions": [
							{"path": "Season 1/old.en.ass", "size": 12, "mtime": "2026-04-20T03:00:00Z"}
						]
					}
				]
			}
		]
	}`)

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	providerSeries := testProviderSeries()
	repo := store.NewRepo()
	result, err := SyncSeries(context.Background(), repo, root, "Bookworm", SeriesSyncOptions{
		ProviderSeries: &providerSeries,
		Inspector:      fakeInspector,
		Replace:        true,
		Apply:          true,
	})
	if err != nil {
		t.Fatalf("SyncSeries: %v", err)
	}
	if len(result.Synced) != 1 || result.Synced[0].Status != "replaced" {
		t.Fatalf("Synced = %#v, want replaced entry", result.Synced)
	}

	trash, err := repo.LoadTrash(seriesDir)
	if err != nil {
		t.Fatalf("LoadTrash: %v", err)
	}
	if len(trash.Entries) != 1 {
		t.Fatalf("len(Trash.Entries) = %d, want 1", len(trash.Entries))
	}
	trashID := trash.Entries[0].ID
	if _, err := ulid.Parse(trashID); err != nil {
		t.Fatalf("Trash ID = %q, want ULID: %v", trashID, err)
	}

	plan, err := PlanSeries(context.Background(), root, "Bookworm", repo)
	if err != nil {
		t.Fatalf("PlanSeries: %v", err)
	}
	if len(plan.FileMoves) != 3 {
		t.Fatalf("len(FileMoves) = %d, want active media plus trashed media/companion", len(plan.FileMoves))
	}
	if err := ApplyPlan(context.Background(), plan, repo); err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	for _, path := range []string{
		filepath.Join(seriesDir, "Season 1", "Bookworm - S01E01 (WebRip 1080p).mkv"),
		filepath.Join(seriesDir, ".kura", "trash", trashID, "old.mkv"),
		filepath.Join(seriesDir, ".kura", "trash", trashID, "old.en.ass"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Stat %s: %v", path, err)
		}
	}
}

func TestPlanAndApplyReconcileRenamesTrackedFiles(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Long Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll .kura: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "old episode.mkv"), "episode")
	writeFile(t, filepath.Join(seasonDir, "old episode.en.ass"), "subtitle")
	writeFile(t, filepath.Join(seriesDir, "bad special.mp4"), "special")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"id": "01JZ7P0Q2V3W4X5Y6Z7A8B9C0D",
		"providerRefs": ["tvdb:370070"],
		"preferredProvider": "tvdb",
		"preferredTitle": "Long Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm",
		"seasons": [
			{
				"number": 1,
				"episodes": [
					{
						"number": 1,
						"media": {
							"path": "Season 1/old episode.mkv",
							"source": "webrip",
							"size": 7,
							"mtime": "2026-04-20T03:00:00Z",
							"mediainfo": {"videoCodec": "HEVC", "resolution": "1920x1080"}
						},
						"companions": [
							{"path": "Season 1/old episode.en.ass", "size": 8, "mtime": "2026-04-20T03:00:00Z"}
						]
					}
				]
			}
		],
		"specials": {
			"number": 0,
			"episodes": [
				{
					"number": 1,
					"media": {
						"path": "bad special.mp4",
						"source": "bdrip",
						"size": 7,
						"mtime": "2026-04-20T03:00:00Z",
						"mediainfo": {"videoCodec": "AVC", "resolution": "1280x720"}
					},
					"companions": []
				}
			]
		}
	}`)

	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	repo := store.NewRepo()
	plan, err := PlanSeries(context.Background(), root, "Long Bookworm", repo)
	if err != nil {
		t.Fatalf("PlanSeries: %v", err)
	}
	if len(plan.FileMoves) != 3 {
		t.Fatalf("len(FileMoves) = %d, want 3", len(plan.FileMoves))
	}
	if err := ApplyPlan(context.Background(), plan, repo); err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	for _, path := range []string{
		filepath.Join(seriesDir, "Season 1", "Long Bookworm - S01E01 (WebRip 1080p).mkv"),
		filepath.Join(seriesDir, "Season 1", "Long Bookworm - S01E01 (WebRip 1080p).en.ass"),
		filepath.Join(seriesDir, "Long Bookworm - S00E01 (BDRip 720p).mp4"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Stat %s: %v", path, err)
		}
	}
}

func TestPlanReconcileTreatsCanonicallyEquivalentRootNameAsUnchanged(t *testing.T) {
	rootPath := t.TempDir()
	dirname := "本好きの下剋上 司書になるためには手段を選んでいられません"
	seriesDir := filepath.Join(rootPath, dirname)
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll .kura: %v", err)
	}
	writeFile(t, filepath.Join(seriesDir, "episode.mkv"), "episode")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"id": "01JZ7P0Q2V3W4X5Y6Z7A8B9C0D",
		"providerRefs": ["tvdb:123"],
		"preferredProvider": "tvdb",
		"preferredTitle": "本好きの下剋上 司書になるためには手段を選んでいられません",
		"canonicalTitle": "Ascendance of a Bookworm",
		"specials": {
			"number": 0,
			"episodes": [
				{
					"number": 1,
					"media": {
						"path": "episode.mkv",
						"source": "webrip",
						"size": 7,
						"mtime": "2026-04-20T03:00:00Z",
						"mediainfo": {"videoCodec": "HEVC", "resolution": "1920x1080"}
					},
					"companions": []
				}
			]
		}
	}`)
	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	plan, err := PlanSeries(context.Background(), root, dirname, store.NewRepo())
	if err != nil {
		t.Fatalf("PlanSeries: %v", err)
	}
	if plan.Target != "本好きの下剋上 司書になるためには手段を選んでいられません" {
		t.Fatalf("Target = %q, want normalized directory name", plan.Target)
	}
	if len(plan.FileMoves) != 1 {
		t.Fatalf("len(FileMoves) = %d, want 1", len(plan.FileMoves))
	}
	if got := plan.FileMoves[0].To; got != "本好きの下剋上 司書になるためには手段を選んでいられません - S00E01 (WebRip 1080p).mkv" {
		t.Fatalf("FileMoves[0].To = %q", got)
	}
}

func TestPlanReconcileUsesCurrentDirectoryName(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Short Title")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll .kura: %v", err)
	}
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "old episode.mkv"), "episode")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"id": "01JZ7P0Q2V3W4X5Y6Z7A8B9C0D",
		"providerRefs": ["tvdb:370070"],
		"preferredProvider": "tvdb",
		"preferredTitle": "A Much Longer Provider Title",
		"canonicalTitle": "Canonical Provider Title",
		"seasons": [
			{
				"number": 1,
				"episodes": [
					{
						"number": 1,
						"media": {
							"path": "Season 1/old episode.mkv",
							"source": "webrip",
							"size": 7,
							"mtime": "2026-04-20T03:00:00Z",
							"mediainfo": {"videoCodec": "HEVC", "resolution": "1920x1080"}
						},
						"companions": []
					}
				]
			}
		]
	}`)
	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	plan, err := PlanSeries(context.Background(), root, "Short Title", store.NewRepo())
	if err != nil {
		t.Fatalf("PlanSeries: %v", err)
	}
	if plan.Target != "Short Title" {
		t.Fatalf("Target = %q, want Short Title", plan.Target)
	}
	if len(plan.FileMoves) != 1 {
		t.Fatalf("len(FileMoves) = %d, want 1", len(plan.FileMoves))
	}
	if got := plan.FileMoves[0].To; got != "Season 1/Short Title - S01E01 (WebRip 1080p).mkv" {
		t.Fatalf("FileMoves[0].To = %q", got)
	}
}

func TestApplyReconcileSkipsUnchangedPlan(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll .kura: %v", err)
	}
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv"), "episode")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"id": "01JZ7P0Q2V3W4X5Y6Z7A8B9C0D",
		"providerRefs": ["tvdb:370070"],
		"preferredProvider": "tvdb",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm",
		"seasons": [
			{
				"number": 1,
				"episodes": [
					{
						"number": 1,
						"media": {
							"path": "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv",
							"source": "webrip",
							"size": 7,
							"mtime": "2026-04-20T03:00:00Z",
							"mediainfo": {"videoCodec": "HEVC", "resolution": "1920x1080"}
						},
						"companions": []
					}
				]
			}
		]
	}`)
	metadataPath := store.SeriesPath(seriesDir)
	originalTime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(metadataPath, originalTime, originalTime); err != nil {
		t.Fatalf("Chtimes series.json: %v", err)
	}
	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	repo := store.NewRepo()
	plan, err := PlanSeries(context.Background(), root, "Bookworm", repo)
	if err != nil {
		t.Fatalf("PlanSeries: %v", err)
	}
	if plan.HasChanges() {
		t.Fatalf("HasChanges = true, want false: %#v", plan)
	}
	if err := ApplyPlan(context.Background(), plan, repo); err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	metadataInfo, err := os.Stat(metadataPath)
	if err != nil {
		t.Fatalf("Stat series.json: %v", err)
	}
	if !metadataInfo.ModTime().Equal(originalTime) {
		t.Fatalf("series.json mtime = %s, want %s", metadataInfo.ModTime(), originalTime)
	}
}

func TestAddEpisodeRecordsRelativeFileFacts(t *testing.T) {
	seriesDir := t.TempDir()
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seasonDir, "episode.mkv"), []byte("episode"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seasonDir, "episode.en.ass"), []byte("subtitle"), 0o644); err != nil {
		t.Fatalf("WriteFile companion: %v", err)
	}

	repo := store.NewRepo()
	series, err := newTestSeries(repo, seriesDir)
	if err != nil {
		t.Fatalf("NewSeries: %v", err)
	}
	updated, err := AddEpisode(seriesDir, *series, AddEpisodeOptions{
		Season:  1,
		Episode: 1,
		Path:    "Season 1/episode.mkv",
		Source:  "WebRip",
		Companions: []string{
			"Season 1/episode.en.ass",
		},
	})
	if err != nil {
		t.Fatalf("AddEpisode: %v", err)
	}

	media := testEpisode(t, &updated, 1, 1).Media
	if media.Path != "Season 1/episode.mkv" {
		t.Fatalf("Media.Path = %q, want Season 1/episode.mkv", media.Path)
	}
	if media.Source != "webrip" {
		t.Fatalf("Media.Source = %q, want webrip", media.Source)
	}
	if media.Size != 7 {
		t.Fatalf("Media.Size = %d, want 7", media.Size)
	}
	if media.MTime == "" {
		t.Fatal("Media.MTime is empty")
	}

	data, err := json.Marshal(testEpisode(t, &updated, 1, 1))
	if err != nil {
		t.Fatalf("Marshal episode: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal episode: %v", err)
	}
	companions, ok := raw["companions"].([]any)
	if !ok {
		t.Fatalf("companions = %T, want array", raw["companions"])
	}
	if len(companions) != 1 {
		t.Fatalf("len(companions) = %d, want 1", len(companions))
	}
	companion := companions[0].(map[string]any)
	if got := companion["path"]; got != "Season 1/episode.en.ass" {
		t.Fatalf("companion.path = %v, want Season 1/episode.en.ass", got)
	}
	if got := companion["size"]; got != float64(8) {
		t.Fatalf("companion.size = %v, want 8", got)
	}
}

func TestAddEpisodeRecordsSpecialsSeparately(t *testing.T) {
	seriesDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(seriesDir, "special.mkv"), []byte("special"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	repo := store.NewRepo()
	series, err := newTestSeries(repo, seriesDir)
	if err != nil {
		t.Fatalf("NewSeries: %v", err)
	}
	updated, err := AddEpisode(seriesDir, *series, AddEpisodeOptions{
		Season:  0,
		Episode: 1,
		Path:    "special.mkv",
	})
	if err != nil {
		t.Fatalf("AddEpisode: %v", err)
	}

	if updated.Specials == nil {
		t.Fatal("Specials = nil, want season")
	}
	if got := testEpisode(t, &updated, 0, 1).Media.Path; got != "special.mkv" {
		t.Fatalf("special media path = %q, want special.mkv", got)
	}
	if _, ok := updated.Season(0); !ok {
		t.Fatal("Specials missing")
	}
	for _, season := range updated.Seasons {
		if season.Number == 0 {
			t.Fatal("Seasons[0] exists, want specials separate")
		}
	}
}

func TestAddEpisodeRejectsExistingEpisodeWithoutReplace(t *testing.T) {
	seriesDir := t.TempDir()
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seasonDir, "episode-1080p.mkv"), []byte("episode 1080p"), 0o644); err != nil {
		t.Fatalf("WriteFile 1080p: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seasonDir, "episode-720p.mkv"), []byte("episode 720p"), 0o644); err != nil {
		t.Fatalf("WriteFile 720p: %v", err)
	}

	repo := store.NewRepo()
	series, err := newTestSeries(repo, seriesDir)
	if err != nil {
		t.Fatalf("NewSeries: %v", err)
	}
	updated, err := AddEpisode(seriesDir, *series, AddEpisodeOptions{
		Season:  1,
		Episode: 1,
		Path:    "Season 1/episode-1080p.mkv",
	})
	if err != nil {
		t.Fatalf("AddEpisode first: %v", err)
	}
	if _, err := AddEpisode(seriesDir, updated, AddEpisodeOptions{
		Season:  1,
		Episode: 1,
		Path:    "Season 1/episode-720p.mkv",
	}); err == nil {
		t.Fatal("AddEpisode second returned nil error, want existing episode error")
	}
}

func TestAddEpisodeReplacesMediaForSameEpisodeWithTrash(t *testing.T) {
	seriesDir := t.TempDir()
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seasonDir, "episode-1080p.mkv"), []byte("episode 1080p"), 0o644); err != nil {
		t.Fatalf("WriteFile 1080p: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seasonDir, "episode-720p.mkv"), []byte("episode 720p"), 0o644); err != nil {
		t.Fatalf("WriteFile 720p: %v", err)
	}

	repo := store.NewRepo()
	series, err := newTestSeries(repo, seriesDir)
	if err != nil {
		t.Fatalf("NewSeries: %v", err)
	}
	updated, err := AddEpisode(seriesDir, *series, AddEpisodeOptions{
		Season:  1,
		Episode: 1,
		Path:    "Season 1/episode-1080p.mkv",
	})
	if err != nil {
		t.Fatalf("AddEpisode first: %v", err)
	}
	trash := store.Trash{SchemaVersion: store.TrashSchemaVersion}
	updated, err = AddEpisode(seriesDir, updated, AddEpisodeOptions{
		Season:  1,
		Episode: 1,
		Path:    "Season 1/episode-720p.mkv",
		Replace: true,
		Trash:   &trash,
	})
	if err != nil {
		t.Fatalf("AddEpisode second: %v", err)
	}

	media := testEpisode(t, &updated, 1, 1).Media
	if media.Path != "Season 1/episode-720p.mkv" {
		t.Fatalf("media path = %q, want replacement path", media.Path)
	}
	if len(trash.Entries) != 1 {
		t.Fatalf("len(Trash.Entries) = %d, want 1", len(trash.Entries))
	}
	if _, err := ulid.Parse(trash.Entries[0].ID); err != nil {
		t.Fatalf("Trash ID = %q, want ULID: %v", trash.Entries[0].ID, err)
	}
	if trash.Entries[0].Season != 1 || trash.Entries[0].Number != 1 {
		t.Fatalf("trash episode = S%02dE%02d, want S01E01", trash.Entries[0].Season, trash.Entries[0].Number)
	}
	if trash.Entries[0].Media.Path != "Season 1/episode-1080p.mkv" {
		t.Fatalf("trash media path = %q, want old path", trash.Entries[0].Media.Path)
	}
}

func TestAddEpisodeRejectsRefreshWithoutReplace(t *testing.T) {
	seriesDir := t.TempDir()
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(seasonDir, "episode.mkv")
	if err := os.WriteFile(path, []byte("episode"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	repo := store.NewRepo()
	series, err := newTestSeries(repo, seriesDir)
	if err != nil {
		t.Fatalf("NewSeries: %v", err)
	}
	updated, err := AddEpisode(seriesDir, *series, AddEpisodeOptions{
		Season:  1,
		Episode: 1,
		Path:    "Season 1/episode.mkv",
	})
	if err != nil {
		t.Fatalf("AddEpisode first: %v", err)
	}
	if err := os.WriteFile(path, []byte("episode updated"), 0o644); err != nil {
		t.Fatalf("WriteFile updated: %v", err)
	}
	if _, err := AddEpisode(seriesDir, updated, AddEpisodeOptions{
		Season:  1,
		Episode: 1,
		Path:    "Season 1/episode.mkv",
	}); err == nil {
		t.Fatal("AddEpisode second returned nil error, want existing episode error")
	}
}

func TestAddEpisodeRejectsEscapingPath(t *testing.T) {
	seriesDir := t.TempDir()
	repo := store.NewRepo()
	series, err := newTestSeries(repo, seriesDir)
	if err != nil {
		t.Fatalf("NewSeries: %v", err)
	}
	if _, err := AddEpisode(seriesDir, *series, AddEpisodeOptions{
		Season:  1,
		Episode: 1,
		Path:    "../episode.mkv",
	}); err == nil {
		t.Fatal("AddEpisode returned nil error, want escaping path error")
	}
}

var fakeInspector = MediaInspectorFunc(func(context.Context, string) (domain.MediaInfo, error) {
	return domain.MediaInfo{
		VideoCodec: "HEVC",
		Resolution: "1920x1080",
	}, nil
})

func testProviderSeries() metadata.Series {
	return metadata.Series{
		SeriesSummary: metadata.SeriesSummary{
			ProviderRef:      "tvdb:370070",
			ProviderRefs:     []string{"tvdb:370070", "imdb:tt10885406", "tmdb:12345"},
			PreferredTitle:   "本好きの下剋上",
			CanonicalTitle:   "Ascendance of a Bookworm",
			OriginalLanguage: "jpn",
		},
		Seasons: []metadata.Season{{
			Number: 1,
			Episodes: []metadata.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1},
				{SeasonNumber: 1, EpisodeNumber: 2},
			},
		}},
		Specials: []metadata.Episode{
			{SeasonNumber: 0, EpisodeNumber: 1},
		},
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func writeSeriesJSON(t *testing.T, seriesDir string, content string) {
	t.Helper()
	if err := os.WriteFile(store.SeriesPath(seriesDir), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile series.json: %v", err)
	}
}

func testEpisode(t *testing.T, series *store.Series, seasonNumber int, episodeNumber int) store.Episode {
	t.Helper()
	episode, ok := series.LookupEpisode(seasonNumber, episodeNumber)
	if !ok {
		t.Fatalf("LookupEpisode(%d, %d) = false", seasonNumber, episodeNumber)
	}
	return episode
}

func newTestSeries(repo store.Repo, seriesDir string) (*store.Series, error) {
	series, err := repo.NewSeries(seriesDir)
	if err != nil {
		return nil, err
	}
	series.ProviderRefs = []string{"tvdb:370070"}
	series.PreferredProvider = "tvdb"
	series.PreferredTitle = "Honzuki"
	series.CanonicalTitle = "Ascendance of a Bookworm"
	return series, nil
}

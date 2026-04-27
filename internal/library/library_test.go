package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/metadata"
)

func TestResolveProviderSeriesExactMatch(t *testing.T) {
	metadataSource := fakeMetadataSource{
		searchResults: []metadata.SearchResult{{
			SeriesSummary: metadata.SeriesSummary{
				ProviderRef:    "tvdb:370070",
				PreferredTitle: "本好きの下剋上",
				CanonicalTitle: "Ascendance of a Bookworm",
			},
		}},
		series: map[string]metadata.Series{
			"370070": testProviderSeries(),
		},
	}

	series, selected, err := ResolveProviderSeries(context.Background(), metadataSource, "本好きの下剋上", ResolveSeriesOptions{})
	if err != nil {
		t.Fatalf("ResolveProviderSeries: %v", err)
	}
	if selected {
		t.Fatal("selected = true, want false for exact search match")
	}
	if series.ProviderRef != "tvdb:370070" {
		t.Fatalf("ProviderRef = %q, want tvdb:370070", series.ProviderRef)
	}
}

func TestResolveProviderSeriesSingleSubstringMatch(t *testing.T) {
	metadataSource := fakeMetadataSource{
		searchResults: []metadata.SearchResult{{
			SeriesSummary: metadata.SeriesSummary{
				ProviderRef:    "tvdb:370070",
				PreferredTitle: "本好きの下剋上 司書になるためには手段を選んでいられません",
				CanonicalTitle: "Ascendance of a Bookworm",
			},
		}},
		series: map[string]metadata.Series{
			"370070": testProviderSeries(),
		},
	}

	series, selected, err := ResolveProviderSeries(context.Background(), metadataSource, "本好きの下剋上", ResolveSeriesOptions{})
	if err != nil {
		t.Fatalf("ResolveProviderSeries: %v", err)
	}
	if selected {
		t.Fatal("selected = true, want false for search match")
	}
	if series.ProviderRef != "tvdb:370070" {
		t.Fatalf("ProviderRef = %q, want tvdb:370070", series.ProviderRef)
	}
}

func TestResolveProviderSeriesDoesNotSubstringMatchMultipleResults(t *testing.T) {
	metadataSource := fakeMetadataSource{
		searchResults: []metadata.SearchResult{
			{
				SeriesSummary: metadata.SeriesSummary{
					ProviderRef:    "tvdb:1",
					PreferredTitle: "Bookworm Extra",
				},
			},
			{
				SeriesSummary: metadata.SeriesSummary{
					ProviderRef:    "tvdb:2",
					CanonicalTitle: "Bookworm OVA",
				},
			},
		},
	}

	_, _, err := ResolveProviderSeries(context.Background(), metadataSource, "Bookworm", ResolveSeriesOptions{})
	selectionRequired, ok := errors.AsType[SeriesSelectionRequiredError](err)
	if !ok {
		t.Fatalf("error = %v, want SeriesSelectionRequiredError", err)
	}
	if len(selectionRequired.Candidates) != 2 {
		t.Fatalf("len(Candidates) = %d, want 2", len(selectionRequired.Candidates))
	}
}

func TestResolveProviderSeriesReturnsCandidatesWhenSelectionRequired(t *testing.T) {
	metadataSource := fakeMetadataSource{
		searchResults: []metadata.SearchResult{{
			SeriesSummary: metadata.SeriesSummary{
				ProviderRef:    "tvdb:1",
				PreferredTitle: "Candidate",
			},
		}},
	}

	_, _, err := ResolveProviderSeries(context.Background(), metadataSource, "No Match", ResolveSeriesOptions{})
	selectionRequired, ok := errors.AsType[SeriesSelectionRequiredError](err)
	if !ok {
		t.Fatalf("error = %v, want SeriesSelectionRequiredError", err)
	}
	if len(selectionRequired.Candidates) != 1 {
		t.Fatalf("len(Candidates) = %d, want 1", len(selectionRequired.Candidates))
	}
}

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

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	providerSeries := testProviderSeries()
	result, err := New().SyncSeries(context.Background(), root, "Bookworm", SeriesSyncOptions{
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

	loaded, err := New().LoadSeries(seriesDir)
	if err != nil {
		t.Fatalf("LoadSeries: %v", err)
	}
	episode := loaded.Seasons["1"].Episodes["1"]
	if episode.Media.Path != "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv" {
		t.Fatalf("Media.Path = %q", episode.Media.Path)
	}
	if len(episode.Companions) != 1 {
		t.Fatalf("len(Companions) = %d, want 1", len(episode.Companions))
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

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	providerSeries := testProviderSeries()
	providerSeries.PreferredTitle = "A Much Longer Provider Title"
	providerSeries.CanonicalTitle = "Canonical Provider Title"
	if _, err := New().SyncSeries(context.Background(), root, "Short Title", SeriesSyncOptions{
		ProviderSeries: &providerSeries,
		Inspector:      fakeInspector,
		Apply:          true,
	}); err != nil {
		t.Fatalf("SyncSeries: %v", err)
	}

	loaded, err := New().LoadSeries(seriesDir)
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

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	result, err := New().SyncSeries(context.Background(), root, "Bookworm", SeriesSyncOptions{})
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

	metadataPath := SeriesPath(seriesDir)
	originalTime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(metadataPath, originalTime, originalTime); err != nil {
		t.Fatalf("Chtimes series.json: %v", err)
	}
	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}

	result, err := New().SyncSeries(context.Background(), root, "Bookworm", SeriesSyncOptions{Apply: true})
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

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	providerSeries := testProviderSeries()
	_, err = New().SyncSeries(context.Background(), root, "Bookworm", SeriesSyncOptions{
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

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	providerSeries := testProviderSeries()
	_, err = New().SyncSeries(context.Background(), root, "Bookworm", SeriesSyncOptions{
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
	series, err := testSeries(seriesDir)
	if err != nil {
		t.Fatalf("testSeries: %v", err)
	}
	if err := New().SaveSeries(*series); err != nil {
		t.Fatalf("SaveSeries: %v", err)
	}
	stageDir := t.TempDir()
	mediaPath := filepath.Join(stageDir, "Bookworm - S01E01 (WebRip).mkv")
	companionPath := filepath.Join(stageDir, "Bookworm - S01E01 (WebRip).en.ass")
	writeFile(t, mediaPath, "episode")
	writeFile(t, companionPath, "subtitle")

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	season, _ := RegularSeason(1)
	episode, _ := NewEpisodeNumber(1)
	providerSeries := testProviderSeries()
	result, err := New().StageEpisodeFile(context.Background(), root, "Bookworm", StageEpisodeFileOptions{
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
	staged, err := New().LoadStaged(seriesDir)
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
	series, err := testSeries(seriesDir)
	if err != nil {
		t.Fatalf("testSeries: %v", err)
	}
	if err := New().SaveSeries(*series); err != nil {
		t.Fatalf("SaveSeries: %v", err)
	}
	stageDir := t.TempDir()
	firstPath := filepath.Join(stageDir, "first.mkv")
	secondPath := filepath.Join(stageDir, "second.mkv")
	writeFile(t, firstPath, "first")
	writeFile(t, secondPath, "second")

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	season, _ := RegularSeason(1)
	episode, _ := NewEpisodeNumber(1)
	providerSeries := testProviderSeries()
	lib := New()
	if _, err := lib.StageEpisodeFile(context.Background(), root, "Bookworm", StageEpisodeFileOptions{
		Season:         season,
		Episode:        episode,
		MediaPath:      firstPath,
		Inspector:      fakeInspector,
		ProviderSeries: &providerSeries,
		Apply:          true,
	}); err != nil {
		t.Fatalf("StageEpisodeFile first: %v", err)
	}
	if _, err := lib.StageEpisodeFile(context.Background(), root, "Bookworm", StageEpisodeFileOptions{
		Season:         season,
		Episode:        episode,
		MediaPath:      secondPath,
		Inspector:      fakeInspector,
		ProviderSeries: &providerSeries,
		Apply:          true,
	}); err == nil {
		t.Fatal("StageEpisodeFile second returned nil error, want staged episode exists error")
	}
	result, err := lib.StageEpisodeFile(context.Background(), root, "Bookworm", StageEpisodeFileOptions{
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
	staged, err := lib.LoadStaged(seriesDir)
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

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	season, _ := RegularSeason(1)
	episode, _ := NewEpisodeNumber(1)
	providerSeries := testProviderSeries()
	lib := New()
	if _, err := lib.StageEpisodeFile(context.Background(), root, "Bookworm", StageEpisodeFileOptions{
		Season:         season,
		Episode:        episode,
		MediaPath:      mediaPath,
		Inspector:      fakeInspector,
		ProviderSeries: &providerSeries,
		Apply:          true,
	}); err == nil {
		t.Fatal("StageEpisodeFile returned nil error, want active episode exists error")
	}
	result, err := lib.StageEpisodeFile(context.Background(), root, "Bookworm", StageEpisodeFileOptions{
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
	staged, err := lib.LoadStaged(seriesDir)
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
	series, err := testSeries(seriesDir)
	if err != nil {
		t.Fatalf("testSeries: %v", err)
	}
	lib := New()
	if err := lib.SaveSeries(*series); err != nil {
		t.Fatalf("SaveSeries: %v", err)
	}

	stageDir := t.TempDir()
	mediaPath := filepath.Join(stageDir, "incoming.mkv")
	companionPath := filepath.Join(stageDir, "incoming.en.ass")
	writeFile(t, mediaPath, "episode")
	writeFile(t, companionPath, "subtitle")
	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	season, _ := RegularSeason(1)
	episode, _ := NewEpisodeNumber(1)
	providerSeries := testProviderSeries()
	if _, err := lib.StageEpisodeFile(context.Background(), root, "Bookworm", StageEpisodeFileOptions{
		Season:         season,
		Episode:        episode,
		Source:         MediaSourceWebRip,
		Companions:     []string{companionPath},
		MediaPath:      mediaPath,
		Inspector:      fakeInspector,
		ProviderSeries: &providerSeries,
		Apply:          true,
	}); err != nil {
		t.Fatalf("StageEpisodeFile: %v", err)
	}

	plan, err := lib.PlanReconcile(context.Background(), root, "Bookworm")
	if err != nil {
		t.Fatalf("PlanReconcile: %v", err)
	}
	if len(plan.FileMoves) != 2 {
		t.Fatalf("len(FileMoves) = %d, want media plus companion", len(plan.FileMoves))
	}
	if err := lib.ApplyReconcile(context.Background(), plan); err != nil {
		t.Fatalf("ApplyReconcile: %v", err)
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
	if _, err := os.Stat(StagedPath(seriesDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat staged.json = %v, want not exist", err)
	}
	loaded, err := lib.LoadSeries(seriesDir)
	if err != nil {
		t.Fatalf("LoadSeries: %v", err)
	}
	if got := loaded.Seasons["1"].Episodes["1"].Media.Path; got != "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv" {
		t.Fatalf("Media.Path = %q, want reconciled path", got)
	}
	if got := loaded.Seasons["1"].Episodes["1"].Companions[0].Path; got != "Season 1/Bookworm - S01E01 (WebRip 1080p).en.ass" {
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

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	season, _ := RegularSeason(1)
	episode, _ := NewEpisodeNumber(1)
	providerSeries := testProviderSeries()
	lib := New()
	if _, err := lib.StageEpisodeFile(context.Background(), root, "Bookworm", StageEpisodeFileOptions{
		Season:         season,
		Episode:        episode,
		Source:         MediaSourceWebRip,
		MediaPath:      mediaPath,
		Inspector:      fakeInspector,
		ProviderSeries: &providerSeries,
		Apply:          true,
		Replace:        true,
	}); err != nil {
		t.Fatalf("StageEpisodeFile replace: %v", err)
	}
	plan, err := lib.PlanReconcile(context.Background(), root, "Bookworm")
	if err != nil {
		t.Fatalf("PlanReconcile: %v", err)
	}
	if len(plan.FileMoves) != 2 {
		t.Fatalf("len(FileMoves) = %d, want trash move plus staged media move", len(plan.FileMoves))
	}
	if err := lib.ApplyReconcile(context.Background(), plan); err != nil {
		t.Fatalf("ApplyReconcile: %v", err)
	}

	trash, err := lib.LoadTrash(seriesDir)
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
	if _, err := os.Stat(StagedPath(seriesDir)); !errors.Is(err, os.ErrNotExist) {
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

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	providerSeries := testProviderSeries()
	lib := New()
	result, err := lib.SyncSeries(context.Background(), root, "Bookworm", SeriesSyncOptions{
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

	trash, err := lib.LoadTrash(seriesDir)
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

	plan, err := lib.PlanReconcile(context.Background(), root, "Bookworm")
	if err != nil {
		t.Fatalf("PlanReconcile: %v", err)
	}
	if len(plan.FileMoves) != 3 {
		t.Fatalf("len(FileMoves) = %d, want active media plus trashed media/companion", len(plan.FileMoves))
	}
	if err := lib.ApplyReconcile(context.Background(), plan); err != nil {
		t.Fatalf("ApplyReconcile: %v", err)
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

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	lib := New()
	plan, err := lib.PlanReconcile(context.Background(), root, "Long Bookworm")
	if err != nil {
		t.Fatalf("PlanReconcile: %v", err)
	}
	if len(plan.FileMoves) != 3 {
		t.Fatalf("len(FileMoves) = %d, want 3", len(plan.FileMoves))
	}
	if err := lib.ApplyReconcile(context.Background(), plan); err != nil {
		t.Fatalf("ApplyReconcile: %v", err)
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
	dirname := "本好きの下剋上 司書になるためには手段を選んでいられません"
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
	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	plan, err := New().PlanReconcile(context.Background(), root, dirname)
	if err != nil {
		t.Fatalf("PlanReconcile: %v", err)
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
	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	plan, err := New().PlanReconcile(context.Background(), root, "Short Title")
	if err != nil {
		t.Fatalf("PlanReconcile: %v", err)
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
	metadataPath := SeriesPath(seriesDir)
	originalTime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(metadataPath, originalTime, originalTime); err != nil {
		t.Fatalf("Chtimes series.json: %v", err)
	}
	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	lib := New()
	plan, err := lib.PlanReconcile(context.Background(), root, "Bookworm")
	if err != nil {
		t.Fatalf("PlanReconcile: %v", err)
	}
	if plan.HasChanges() {
		t.Fatalf("HasChanges = true, want false: %#v", plan)
	}
	if err := lib.ApplyReconcile(context.Background(), plan); err != nil {
		t.Fatalf("ApplyReconcile: %v", err)
	}
	metadataInfo, err := os.Stat(metadataPath)
	if err != nil {
		t.Fatalf("Stat series.json: %v", err)
	}
	if !metadataInfo.ModTime().Equal(originalTime) {
		t.Fatalf("series.json mtime = %s, want %s", metadataInfo.ModTime(), originalTime)
	}
}

type fakeMetadataSource struct {
	searchResults []metadata.SearchResult
	series        map[string]metadata.Series
}

func (p fakeMetadataSource) Key() string {
	return "tvdb"
}

func (p fakeMetadataSource) Search(context.Context, string, metadata.SearchOptions) ([]metadata.SearchResult, error) {
	return slices.Clone(p.searchResults), nil
}

func (p fakeMetadataSource) GetSeries(_ context.Context, providerID string) (metadata.Series, error) {
	series, ok := p.series[providerID]
	if !ok {
		return metadata.Series{}, fmt.Errorf("series %s not found", providerID)
	}
	return series, nil
}

var fakeInspector = MediaInspectorFunc(func(context.Context, string) (MediaInfo, error) {
	return MediaInfo{
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
	if err := os.WriteFile(SeriesPath(seriesDir), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile series.json: %v", err)
	}
}

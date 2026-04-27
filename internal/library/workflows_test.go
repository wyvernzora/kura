package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

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
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip HEVC 1920x1080).mkv"), "episode 1")
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip HEVC 1920x1080).en.ass"), "subtitle")
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E02.mkv"), "episode 2")
	writeFile(t, filepath.Join(seasonDir, "Bookworm - bonus.mkv"), "bonus")

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	providerSeries := testProviderSeries()
	result, err := New().SyncSeries(context.Background(), root, "Bookworm", SeriesSyncOptions{
		ProviderSeries:          &providerSeries,
		PreserveFilesystemTitle: true,
		Inspector:               fakeInspector{},
		Apply:                   true,
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
	if loaded.FilesystemTitle != "Bookworm" {
		t.Fatalf("FilesystemTitle = %q, want Bookworm", loaded.FilesystemTitle)
	}
	episode := loaded.Seasons["1"].Episodes["1"]
	if episode.Media.Path != "Season 1/Bookworm - S01E01 (WebRip HEVC 1920x1080).mkv" {
		t.Fatalf("Media.Path = %q", episode.Media.Path)
	}
	if len(episode.Companions) != 1 {
		t.Fatalf("len(Companions) = %d, want 1", len(episode.Companions))
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
	episodePath := filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip HEVC 1920x1080).mkv")
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
							"path": "Season 1/Bookworm - S01E01 (WebRip HEVC 1920x1080).mkv",
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
}

func TestImportEpisodeFileFindsSeriesAndRecordsCompanion(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	series, err := testSeries(seriesDir)
	if err != nil {
		t.Fatalf("testSeries: %v", err)
	}
	if err := New().SaveSeries(*series); err != nil {
		t.Fatalf("SaveSeries: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "episode.mkv"), "episode")
	writeFile(t, filepath.Join(seasonDir, "episode.en.ass"), "subtitle")

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	season, _ := RegularSeason(1)
	episode, _ := NewEpisodeNumber(1)
	updated, err := New().ImportEpisodeFile(context.Background(), root, ImportEpisodeFileOptions{
		Season:     season,
		Episode:    episode,
		Companions: []string{"Bookworm/Season 1/episode.en.ass"},
		MediaPath:  "Bookworm/Season 1/episode.mkv",
		Inspector:  fakeInspector{},
		Apply:      true,
	})
	if err != nil {
		t.Fatalf("ImportEpisodeFile: %v", err)
	}
	got := updated.Seasons["1"].Episodes["1"]
	if got.Media.Path != "Season 1/episode.mkv" {
		t.Fatalf("Media.Path = %q", got.Media.Path)
	}
	if len(got.Companions) != 1 || got.Companions[0].Path != "Season 1/episode.en.ass" {
		t.Fatalf("Companions = %#v", got.Companions)
	}
}

func TestPlanAndApplyReconcileRenamesTrackedFilesThenRoot(t *testing.T) {
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
		"filesystemTitle": "Bookworm",
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
	if plan.RootMove == nil || plan.RootMove.To != "Bookworm" {
		t.Fatalf("RootMove = %#v, want Bookworm", plan.RootMove)
	}
	if err := lib.ApplyReconcile(context.Background(), plan); err != nil {
		t.Fatalf("ApplyReconcile: %v", err)
	}
	targetSeriesDir := filepath.Join(rootPath, "Bookworm")
	if _, err := os.Stat(seriesDir); !os.IsNotExist(err) {
		t.Fatalf("old series dir stat err = %v, want not exists", err)
	}
	for _, path := range []string{
		filepath.Join(targetSeriesDir, "Season 1", "Bookworm - S01E01 (WebRip HEVC 1920x1080).mkv"),
		filepath.Join(targetSeriesDir, "Season 1", "Bookworm - S01E01 (WebRip HEVC 1920x1080).en.ass"),
		filepath.Join(targetSeriesDir, "Bookworm - S00E01 (BDRip AVC 1280x720).mp4"),
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
	if plan.RootMove != nil {
		t.Fatalf("RootMove = %#v, want nil", plan.RootMove)
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

type fakeInspector struct{}

func (fakeInspector) Inspect(context.Context, string) (MediaInfo, error) {
	return MediaInfo{
		VideoCodec: "HEVC",
		Resolution: "1920x1080",
	}, nil
}

func testProviderSeries() metadata.Series {
	return metadata.Series{
		SeriesSummary: metadata.SeriesSummary{
			ProviderRef:      "tvdb:370070",
			ProviderRefs:     []string{"tvdb:370070", "imdb:tt10885406", "tmdb:12345"},
			PreferredTitle:   "本好きの下剋上",
			CanonicalTitle:   "Ascendance of a Bookworm",
			OriginalLanguage: "jpn",
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

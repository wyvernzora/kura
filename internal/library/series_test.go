package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oklog/ulid/v2"
)

func TestSaveLoadSeries(t *testing.T) {
	seriesDir := t.TempDir()
	lib := New()

	series, err := lib.NewSeries(seriesDir)
	if err != nil {
		t.Fatalf("NewSeries: %v", err)
	}
	series.ProviderRefs = []string{"tvdb:370070"}
	series.PreferredProvider = "tvdb"
	series.PreferredTitle = "本好きの下剋上"
	series.CanonicalTitle = "Ascendance of a Bookworm"
	series.FilesystemTitle = "Bookworm"
	if err := lib.SaveSeries(*series); err != nil {
		t.Fatalf("SaveSeries: %v", err)
	}

	got, err := lib.LoadSeries(seriesDir)
	if err != nil {
		t.Fatalf("LoadSeries: %v", err)
	}
	if got.SchemaVersion != SeriesSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", got.SchemaVersion, SeriesSchemaVersion)
	}
	if got.ID != series.ID {
		t.Fatalf("ID = %q, want %q", got.ID, series.ID)
	}
	if _, err := ulid.Parse(got.ID); err != nil {
		t.Fatalf("ID %q is not a valid ULID: %v", got.ID, err)
	}
	if !slices.Equal(got.ProviderRefs, []string{"tvdb:370070"}) {
		t.Fatalf("ProviderRefs = %#v, want [tvdb:370070]", got.ProviderRefs)
	}
	if got.PreferredProvider != "tvdb" {
		t.Fatalf("PreferredProvider = %q, want tvdb", got.PreferredProvider)
	}
	if got.PreferredTitle != "本好きの下剋上" {
		t.Fatalf("PreferredTitle = %q, want 本好きの下剋上", got.PreferredTitle)
	}
	if got.CanonicalTitle != "Ascendance of a Bookworm" {
		t.Fatalf("CanonicalTitle = %q, want Ascendance of a Bookworm", got.CanonicalTitle)
	}
	if got.FilesystemTitle != "Bookworm" {
		t.Fatalf("FilesystemTitle = %q, want Bookworm", got.FilesystemTitle)
	}
}

func TestLoadSeriesRejectsFutureSchema(t *testing.T) {
	seriesDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(SeriesPath(seriesDir), []byte(`{"schemaVersion":2,"id":"x","providerRefs":["tvdb:1"],"preferredProvider":"tvdb","preferredTitle":"x","canonicalTitle":"x"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := New().LoadSeries(seriesDir); err == nil {
		t.Fatal("LoadSeries returned nil error, want future schema error")
	}
}

func TestLoadSeriesRejectsSchemaInvalidDocument(t *testing.T) {
	seriesDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(SeriesPath(seriesDir), []byte(`{"schemaVersion":1,"id":"x","providerRefs":["tvdb:1"],"preferredProvider":"tvdb","preferredTitle":"x","canonicalTitle":"x","externalIds":{"tvdb":"1"}}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := New().LoadSeries(seriesDir); err == nil {
		t.Fatal("LoadSeries returned nil error, want schema validation error")
	}
}

func TestSaveSeriesRejectsUnboundSeries(t *testing.T) {
	series := Series{
		SchemaVersion:     SeriesSchemaVersion,
		ID:                "01JZ7P0Q2V3W4X5Y6Z7A8B9C0D",
		ProviderRefs:      []string{"tvdb:370070"},
		PreferredProvider: "tvdb",
		PreferredTitle:    "Honzuki",
		CanonicalTitle:    "Ascendance of a Bookworm",
	}

	if err := New().SaveSeries(series); err == nil {
		t.Fatal("SaveSeries returned nil error, want unbound series error")
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

	series, err := testSeries(seriesDir)
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

	media := updated.Seasons["1"].Episodes["1"].Media
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

	data, err := json.Marshal(updated.Seasons["1"].Episodes["1"])
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

	series, err := testSeries(seriesDir)
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
	if got := updated.Specials.Episodes["1"].Media.Path; got != "special.mkv" {
		t.Fatalf("special media path = %q, want special.mkv", got)
	}
	if _, ok := updated.Seasons["0"]; ok {
		t.Fatal("Seasons[0] exists, want specials separate")
	}
}

func TestAddEpisodeReplacesMediaForSameEpisode(t *testing.T) {
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

	series, err := testSeries(seriesDir)
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
	updated, err = AddEpisode(seriesDir, updated, AddEpisodeOptions{
		Season:  1,
		Episode: 1,
		Path:    "Season 1/episode-720p.mkv",
	})
	if err != nil {
		t.Fatalf("AddEpisode second: %v", err)
	}

	media := updated.Seasons["1"].Episodes["1"].Media
	if media.Path != "Season 1/episode-720p.mkv" {
		t.Fatalf("media path = %q, want replacement path", media.Path)
	}
}

func TestAddEpisodeRefreshesExistingMediaPath(t *testing.T) {
	seriesDir := t.TempDir()
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(seasonDir, "episode.mkv")
	if err := os.WriteFile(path, []byte("episode"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	series, err := testSeries(seriesDir)
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
	updated, err = AddEpisode(seriesDir, updated, AddEpisodeOptions{
		Season:  1,
		Episode: 1,
		Path:    "Season 1/episode.mkv",
	})
	if err != nil {
		t.Fatalf("AddEpisode second: %v", err)
	}

	media := updated.Seasons["1"].Episodes["1"].Media
	if media.Size != int64(len("episode updated")) {
		t.Fatalf("media size = %d, want updated size", media.Size)
	}
}

func TestLoadSeriesRejectsDuplicateEpisodeNumber(t *testing.T) {
	seriesDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(seriesDir, "Season 1"), 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seriesDir, "Season 1", "first.mkv"), []byte("first"), 0o644); err != nil {
		t.Fatalf("WriteFile first: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seriesDir, "Season 1", "second.mkv"), []byte("second"), 0o644); err != nil {
		t.Fatalf("WriteFile second: %v", err)
	}
	if err := os.WriteFile(SeriesPath(seriesDir), []byte(`{
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
						"media": {"path": "Season 1/first.mkv", "source": "unknown", "size": 5, "mtime": "2026-04-20T03:00:00Z"},
						"companions": []
					},
					{
						"number": 1,
						"media": {"path": "Season 1/second.mkv", "source": "unknown", "size": 6, "mtime": "2026-04-20T03:00:00Z"},
						"companions": []
					}
				]
			}
		]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile series.json: %v", err)
	}

	if _, err := New().LoadSeries(seriesDir); err == nil {
		t.Fatal("LoadSeries returned nil error, want duplicate episode rejection")
	}
}

func TestAddEpisodeRejectsEscapingPath(t *testing.T) {
	seriesDir := t.TempDir()
	series, err := testSeries(seriesDir)
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

func TestSeriesJSONUsesPersistentFieldNames(t *testing.T) {
	series, err := testSeries(t.TempDir())
	if err != nil {
		t.Fatalf("NewSeries: %v", err)
	}
	data, err := json.Marshal(series)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := raw["schemaVersion"]; !ok {
		t.Fatal("schemaVersion missing")
	}
	if _, ok := raw["providerRefs"]; !ok {
		t.Fatal("providerRefs missing")
	}
	if _, ok := raw["preferredProvider"]; !ok {
		t.Fatal("preferredProvider missing")
	}
	if _, ok := raw["preferredTitle"]; !ok {
		t.Fatal("preferredTitle missing")
	}
	if _, ok := raw["canonicalTitle"]; !ok {
		t.Fatal("canonicalTitle missing")
	}
	if _, ok := raw["filesystemTitle"]; ok {
		t.Fatal("filesystemTitle present, want omitted when unset")
	}
	if _, ok := raw["externalIds"]; ok {
		t.Fatal("externalIds present, want providerRefs")
	}
	if _, ok := raw["title"]; ok {
		t.Fatal("title present, want preferredTitle/canonicalTitle")
	}
}

func testSeries(seriesDir string) (*Series, error) {
	series, err := New().NewSeries(seriesDir)
	if err != nil {
		return nil, err
	}
	series.ProviderRefs = []string{"tvdb:370070"}
	series.PreferredProvider = "tvdb"
	series.PreferredTitle = "Honzuki"
	series.CanonicalTitle = "Ascendance of a Bookworm"
	return series, nil
}

package store

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

	series, err := NewSeries(seriesDir)
	if err != nil {
		t.Fatalf("NewSeries: %v", err)
	}
	series.ProviderRefs = []string{"tvdb:370070"}
	series.PreferredProvider = "tvdb"
	series.PreferredTitle = "本好きの下剋上"
	series.CanonicalTitle = "Ascendance of a Bookworm"
	if err := SaveSeries(*series); err != nil {
		t.Fatalf("SaveSeries: %v", err)
	}

	got, err := LoadSeries(seriesDir)
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
}

func TestLoadSeriesRejectsFutureSchema(t *testing.T) {
	seriesDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(SeriesPath(seriesDir), []byte(`{"schemaVersion":2,"id":"x","providerRefs":["tvdb:1"],"preferredProvider":"tvdb","preferredTitle":"x","canonicalTitle":"x"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := LoadSeries(seriesDir); err == nil {
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

	if _, err := LoadSeries(seriesDir); err == nil {
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

	if err := SaveSeries(series); err == nil {
		t.Fatal("SaveSeries returned nil error, want unbound series error")
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

	if _, err := LoadSeries(seriesDir); err == nil {
		t.Fatal("LoadSeries returned nil error, want duplicate episode rejection")
	}
}

func TestSeriesJSONUsesPersistentFieldNames(t *testing.T) {
	series, err := newTestSeries(t.TempDir())
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

func newTestSeries(seriesDir string) (*Series, error) {
	series, err := NewSeries(seriesDir)
	if err != nil {
		return nil, err
	}
	series.ProviderRefs = []string{"tvdb:370070"}
	series.PreferredProvider = "tvdb"
	series.PreferredTitle = "Honzuki"
	series.CanonicalTitle = "Ascendance of a Bookworm"
	return series, nil
}

package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadSeries(t *testing.T) {
	seriesDir := t.TempDir()

	series, err := NewSeries(seriesDir)
	if err != nil {
		t.Fatalf("NewSeries: %v", err)
	}
	series.MetadataRef = "tvdb:370070"
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
	if got.MetadataRef != "tvdb:370070" {
		t.Fatalf("MetadataRef = %q, want tvdb:370070", got.MetadataRef)
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
	if err := os.WriteFile(SeriesPath(seriesDir), []byte(`{"schemaVersion":2,"metadataRef":"tvdb:1","preferredTitle":"x","canonicalTitle":"x"}`), 0o644); err != nil {
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
	if err := os.WriteFile(SeriesPath(seriesDir), []byte(`{"schemaVersion":1,"metadataRef":"tvdb:1","preferredTitle":"x","canonicalTitle":"x","externalIds":{"tvdb":"1"}}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := LoadSeries(seriesDir); err == nil {
		t.Fatal("LoadSeries returned nil error, want schema validation error")
	}
}

func TestSaveSeriesRejectsUnboundSeries(t *testing.T) {
	series := Series{
		SchemaVersion:  SeriesSchemaVersion,
		MetadataRef:    "tvdb:370070",
		PreferredTitle: "Honzuki",
		CanonicalTitle: "Ascendance of a Bookworm",
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
		"metadataRef": "tvdb:370070",
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
	if _, ok := raw["metadataRef"]; !ok {
		t.Fatal("metadataRef missing")
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
		t.Fatal("externalIds present, want metadataRef")
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
	series.MetadataRef = "tvdb:370070"
	series.PreferredTitle = "Honzuki"
	series.CanonicalTitle = "Ascendance of a Bookworm"
	return series, nil
}

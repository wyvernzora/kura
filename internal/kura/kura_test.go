package kura

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	seriespkg "github.com/wyvernzora/kura/internal/series"
)

func TestNewValidatesConfig(t *testing.T) {
	root := t.TempDir()
	if _, err := New(Config{Root: root, TVDBKey: "key"}); err != nil {
		t.Fatalf("New happy path: %v", err)
	}

	_, err := New(Config{Root: filepath.Join(root, "missing"), TVDBKey: "key"})
	if !errors.Is(err, ErrRootNotFound) {
		t.Fatalf("New missing root error = %v, want ErrRootNotFound", err)
	}

	fileRoot := filepath.Join(root, "file")
	writeFile(t, fileRoot, "not a directory")
	_, err = New(Config{Root: fileRoot, TVDBKey: "key"})
	if !errors.Is(err, ErrRootNotDirectory) {
		t.Fatalf("New file root error = %v, want ErrRootNotDirectory", err)
	}

	_, err = New(Config{Root: root})
	if !errors.Is(err, ErrMissingTVDBKey) {
		t.Fatalf("New missing key error = %v, want ErrMissingTVDBKey", err)
	}
}

func TestResolve(t *testing.T) {
	server := newTestTVDBServer(t)
	defer server.Close()

	lib := newTestLibrary(t, t.TempDir(), server.URL)
	resolution, err := lib.Resolve(context.Background(), ResolveInput{Terms: []string{"bookworm"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolution.Results) != 1 {
		t.Fatalf("len(Results) = %d, want 1", len(resolution.Results))
	}
	if got := resolution.Results[0].Summary.MetadataRef; got != "tvdb:370070" {
		t.Fatalf("MetadataRef = %q, want tvdb:370070", got)
	}
}

func TestFindAndGet(t *testing.T) {
	server := newTestTVDBServer(t)
	defer server.Close()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Untracked"), 0o755); err != nil {
		t.Fatalf("Mkdir Untracked: %v", err)
	}
	writeSeriesJSON(t, filepath.Join(root, "Bookworm"), `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S01E0001": {
				"season": 1,
				"episode": 1,
				"airDate": "2019-10-03",
				"active": {
					"path": "Season 1/Bookworm - S01E01.mkv",
					"source": "webrip",
					"size": 7,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": [
						{"path": "Season 1/Bookworm - S01E01.en.ass", "size": 3, "mtime": "2026-04-20T03:00:00Z"}
					]
				}
			}
		}
	}`)
	lib := newTestLibrary(t, root, server.URL)

	_, err := lib.Find("tvdb:999999")
	var notIndexed seriespkg.MetadataRefNotIndexedError
	if !errors.As(err, &notIndexed) {
		t.Fatalf("Find missing error = %v, want MetadataRefNotIndexedError", err)
	}

	_, err = lib.Get("Missing")
	var notFound seriespkg.SeriesNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Get missing error = %v, want SeriesNotFoundError", err)
	}

	_, err = lib.Get("Untracked")
	var notTracked seriespkg.SeriesNotTrackedError
	if !errors.As(err, &notTracked) {
		t.Fatalf("Get untracked error = %v, want SeriesNotTrackedError", err)
	}

	series, err := lib.Find("tvdb:370070")
	if err != nil {
		t.Fatalf("Find tracked: %v", err)
	}
	if series.Ref() != "Bookworm" {
		t.Fatalf("series ref = %q, want Bookworm", series.Ref())
	}
	model, err := series.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if model.Metadata != "tvdb:370070" {
		t.Fatalf("metadata = %q, want tvdb:370070", model.Metadata)
	}
}

func TestAdd(t *testing.T) {
	server := newTestTVDBServer(t)
	defer server.Close()

	root := t.TempDir()
	lib := newTestLibrary(t, root, server.URL)
	series, err := lib.Add(context.Background(), AddInput{MetadataRef: "tvdb:370070"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if series.Ref() != "Bookworm" {
		t.Fatalf("Ref = %q, want Bookworm", series.Ref())
	}
	if _, err := os.Stat(filepath.Join(root, "Bookworm", ".kura", "series.json")); err != nil {
		t.Fatalf("Stat series.json: %v", err)
	}
}

func TestAddRejectsCollisionsAndUnsupportedSource(t *testing.T) {
	server := newTestTVDBServer(t)
	defer server.Close()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Bookworm"), 0o755); err != nil {
		t.Fatalf("Mkdir Bookworm: %v", err)
	}
	lib := newTestLibrary(t, root, server.URL)
	_, err := lib.Add(context.Background(), AddInput{MetadataRef: "tvdb:370070", Ref: "Bookworm"})
	var exists seriespkg.SeriesAlreadyExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("Add existing dir error = %v, want SeriesAlreadyExistsError", err)
	}

	root = t.TempDir()
	writeSeriesJSON(t, filepath.Join(root, "Bookworm"), `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {}
	}`)
	lib = newTestLibrary(t, root, server.URL)
	_, err = lib.Add(context.Background(), AddInput{MetadataRef: "tvdb:370070", Ref: "Other"})
	var conflict seriespkg.MetadataRefConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Add conflict error = %v, want MetadataRefConflictError", err)
	}

	_, err = lib.Add(context.Background(), AddInput{MetadataRef: "imdb:tt123", Ref: "Other"})
	var unsupported seriespkg.UnsupportedMetadataSourceError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Add unsupported error = %v, want UnsupportedMetadataSourceError", err)
	}
}

func TestImport(t *testing.T) {
	server := newTestTVDBServer(t)
	defer server.Close()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Bookworm"), 0o755); err != nil {
		t.Fatalf("Mkdir Bookworm: %v", err)
	}
	lib := newTestLibrary(t, root, server.URL)
	series, err := lib.Import(context.Background(), ImportInput{Ref: "Bookworm", MetadataRef: "tvdb:370070"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	model, err := series.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if model.Metadata != "tvdb:370070" {
		t.Fatalf("MetadataRef = %q, want tvdb:370070", model.Metadata)
	}

	_, err = lib.Import(context.Background(), ImportInput{Ref: "Missing", MetadataRef: "tvdb:370070"})
	var notFound seriespkg.SeriesNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Import missing error = %v, want SeriesNotFoundError", err)
	}

	_, err = lib.Import(context.Background(), ImportInput{Ref: "Bookworm", MetadataRef: "tvdb:370070"})
	var tracked seriespkg.SeriesAlreadyTrackedError
	if !errors.As(err, &tracked) {
		t.Fatalf("Import tracked error = %v, want SeriesAlreadyTrackedError", err)
	}
}

func newTestLibrary(t *testing.T, root string, tvdbBaseURL string) *Library {
	t.Helper()
	lib, err := New(Config{
		Root:               root,
		TVDBKey:            "key",
		TVDBBaseURL:        tvdbBaseURL,
		PreferredLanguages: []string{"eng"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return lib
}

func newTestTVDBServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data":   map[string]any{"token": "token"},
		})
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": []map[string]any{
				{
					"id":             370070,
					"tvdb_id":        "370070",
					"name":           "Bookworm",
					"type":           "series",
					"year":           2019,
					"first_air_time": "2019-10-03",
				},
			},
		})
	})
	mux.HandleFunc("/series/370070/extended", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": map[string]any{
				"id":               370070,
				"name":             "Bookworm",
				"firstAired":       "2019-10-03",
				"lastAired":        "2022-06-14",
				"originalCountry":  "jpn",
				"originalLanguage": "jpn",
				"status":           map[string]any{"name": "Ended"},
				"seasons":          []map[string]any{{"id": 10, "number": 1, "name": "Season 1"}},
			},
		})
	})
	mux.HandleFunc("/series/370070/episodes/default", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": map[string]any{
				"episodes": []map[string]any{
					{
						"id":           1001,
						"name":         "A World Without Books",
						"aired":        "2019-10-03",
						"number":       1,
						"seasonNumber": 1,
					},
				},
			},
			"links": map[string]any{},
		})
	})
	return httptest.NewServer(mux)
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode json: %v", err)
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
	if err := os.MkdirAll(filepath.Join(seriesDir, ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll .kura: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seriesDir, ".kura", "series.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile series.json: %v", err)
	}
}

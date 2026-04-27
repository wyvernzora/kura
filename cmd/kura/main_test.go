package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMetaSearchPrintsJSON(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"meta",
		"search",
		"--tvdb-base-url", server.URL,
		"--limit", "1",
		"honzuki",
	}, testRunContext(&stdout, &stderr))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}

	var results []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if got := results[0]["ProviderRef"]; got != "tvdb:370070" {
		t.Fatalf("ProviderRef = %v, want tvdb:370070", got)
	}
}

func TestSeriesSyncCommandInitializesAndWritesProviderMetadata(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	if err := os.Mkdir(seriesDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"series",
		"sync",
		"--yes",
		"--tvdb-base-url", server.URL,
		"--provider-ref", "tvdb:370070",
		"Bookworm",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(seriesDir, ".kura", "series.json"))
	if err != nil {
		t.Fatalf("ReadFile series.json: %v", err)
	}
	var series map[string]any
	if err := json.Unmarshal(data, &series); err != nil {
		t.Fatalf("unmarshal series.json: %v", err)
	}
	if got := series["preferredProvider"]; got != "tvdb" {
		t.Fatalf("preferredProvider = %v, want tvdb", got)
	}
	if _, ok := series["filesystemTitle"]; ok {
		t.Fatal("filesystemTitle present, want derived from directory name")
	}
	if len(stdout.Bytes()) == 0 {
		t.Fatal("stdout is empty, want written series document")
	}
}

func TestSeriesSyncCommandWritesSummaryAndMetadata(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	mediainfoCommand := newFakeMediaInfoCommand(t, root)
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv"), "episode")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"series",
		"sync",
		"--yes",
		"--tvdb-base-url", server.URL,
		"--provider-ref", "tvdb:370070",
		"Bookworm",
	}, testRunContextWithLibraryRootAndMediaInfo(&stdout, &stderr, root, mediainfoCommand))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "STATUS") || !strings.Contains(stdout.String(), "WebRip") {
		t.Fatalf("stdout = %q, want sync summary", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(seriesDir, ".kura", "series.json")); err != nil {
		t.Fatalf("Stat series.json: %v", err)
	}
}

func TestSeriesSyncCommandDoesNotPromptWhenNothingChanged(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
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

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = run([]string{
		"series",
		"sync",
		"Bookworm",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "Apply this sync?") {
		t.Fatalf("stderr = %q, want no apply prompt", stderr.String())
	}
}

func TestSeriesReconcileCommandPrintsDryRunJSON(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Long Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "old episode.mkv"), "episode")
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
						"companions": []
					}
				]
			}
		]
	}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"series",
		"reconcile",
		"--dry-run",
		"--json",
		"Long Bookworm",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	var plan map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	if got := plan["dryRun"]; got != true {
		t.Fatalf("dryRun = %v, want true", got)
	}
	if moves := plan["fileMoves"].([]any); len(moves) != 1 {
		t.Fatalf("len(fileMoves) = %d, want 1", len(moves))
	}
}

func TestSeriesReconcileCommandDoesNotPromptWhenNothingChanged(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
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

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"series",
		"reconcile",
		"Bookworm",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "Apply these changes?") {
		t.Fatalf("stderr = %q, want no apply prompt", stderr.String())
	}
}

func TestEpisodeImportCommandPrintsUpdatedSeriesJSON(t *testing.T) {
	root := t.TempDir()
	mediainfoCommand := newFakeMediaInfoCommand(t, root)
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"id": "01JZ7P0Q2V3W4X5Y6Z7A8B9C0D",
		"providerRefs": ["tvdb:370070"],
		"preferredProvider": "tvdb",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm"
	}`)
	writeFile(t, filepath.Join(seasonDir, "episode.mkv"), "episode")
	writeFile(t, filepath.Join(seasonDir, "episode.en.ass"), "subtitle")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"episode",
		"import",
		"--season", "1",
		"--number", "1",
		"--companion", "Bookworm/Season 1/episode.en.ass",
		"--dry-run",
		"Bookworm/Season 1/episode.mkv",
	}, testRunContextWithLibraryRootAndMediaInfo(&stdout, &stderr, root, mediainfoCommand))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	var series map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &series); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	season := findJSONNumberedObject(t, series["seasons"], 1)
	episode := findJSONNumberedObject(t, season["episodes"], 1)
	media := episode["media"].(map[string]any)
	if got := media["path"]; got != "Season 1/episode.mkv" {
		t.Fatalf("media.path = %v, want Season 1/episode.mkv", got)
	}
}

func testRunContext(stdout, stderr *bytes.Buffer) runContext {
	return runContext{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: stderr,
		Getenv: func(key string) string {
			switch key {
			case "KURA_TVDB_KEY":
				return "key"
			case "KURA_PREFERRED_LANGUAGES":
				return "ja,en"
			default:
				return ""
			}
		},
	}
}

func testRunContextWithLibraryRoot(stdout, stderr *bytes.Buffer, libraryRoot string) runContext {
	rt := testRunContext(stdout, stderr)
	rt.Getenv = func(key string) string {
		switch key {
		case "KURA_LIBRARY_ROOT":
			return libraryRoot
		case "KURA_TVDB_KEY":
			return "key"
		case "KURA_PREFERRED_LANGUAGES":
			return "ja,en"
		default:
			return ""
		}
	}
	return rt
}

func testRunContextWithLibraryRootAndMediaInfo(stdout, stderr *bytes.Buffer, libraryRoot string, mediaInfoCommand string) runContext {
	rt := testRunContextWithLibraryRoot(stdout, stderr, libraryRoot)
	baseGetenv := rt.Getenv
	rt.Getenv = func(key string) string {
		if key == "KURA_MEDIAINFO_COMMAND" {
			return mediaInfoCommand
		}
		return baseGetenv(key)
	}
	return rt
}

func newCLITestServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": map[string]any{
				"token": "token",
			},
		})
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": []map[string]any{
				{
					"id":             370070,
					"tvdb_id":        "370070",
					"name":           "Ascendance of a Bookworm",
					"type":           "series",
					"year":           2019,
					"first_air_time": "2019-10-03",
					"genres":         nil,
					"translations": map[string]any{
						"jpn": "本好きの下剋上",
						"eng": "Ascendance of a Bookworm",
					},
				},
			},
		})
	})
	mux.HandleFunc("/series/370070/extended", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": map[string]any{
				"id":               370070,
				"name":             "Ascendance of a Bookworm",
				"firstAired":       "2019-10-03",
				"lastAired":        "2022-06-14",
				"originalCountry":  "jpn",
				"originalLanguage": "jpn",
				"averageRuntime":   24,
				"status":           map[string]any{"name": "Ended"},
				"translations": map[string]any{
					"nameTranslations": []map[string]any{
						{"language": "jpn", "name": "本好きの下剋上"},
						{"language": "eng", "name": "Ascendance of a Bookworm"},
					},
				},
				"genres": []map[string]any{
					{"name": "Fantasy"},
				},
				"remoteIds": []map[string]any{
					{"id": "tt10885406", "sourceName": "IMDB"},
					{"id": "12345", "sourceName": "TheMovieDB.com"},
				},
				"seasons": []map[string]any{
					{"id": 10, "number": 1, "name": "Season 1"},
				},
			},
		})
	})
	mux.HandleFunc("/series/370070/episodes/default", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": map[string]any{
				"episodes": []map[string]any{
					{
						"id":             1001,
						"name":           "A World Without Books",
						"aired":          "2019-10-03",
						"number":         1,
						"seasonNumber":   1,
						"absoluteNumber": 1,
						"runtime":        24,
					},
				},
			},
			"links": map[string]any{},
		})
	})
	return httptest.NewServer(mux)
}

func requireAuth(t *testing.T, r *http.Request) {
	t.Helper()

	if got := r.Header.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("Authorization = %q, want Bearer token", got)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}

func findJSONNumberedObject(t *testing.T, value any, number int) map[string]any {
	t.Helper()

	items, ok := value.([]any)
	if !ok {
		t.Fatalf("value = %T, want array", value)
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("item = %T, want object", item)
		}
		if got := object["number"]; got == float64(number) {
			return object
		}
	}
	t.Fatalf("number %d not found in %#v", number, value)
	return nil
}

func newFakeMediaInfoCommand(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(dir, "fake-mediainfo")
	script := `#!/bin/sh
cat <<'JSON'
{
  "media": {
    "track": [
      {"@type": "Video", "Format": "HEVC", "Width": "1920", "Height": "1080"},
      {"@type": "Audio", "Format": "FLAC", "Language": "ja"},
      {"@type": "Text", "Language": "en", "Title": "Signs"}
    ]
  }
}
JSON
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile fake-mediainfo: %v", err)
	}
	return path
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

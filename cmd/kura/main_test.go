package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/store"
	"github.com/wyvernzora/kura/internal/ui"
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

	var resolution map[string][]map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &resolution); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	results := resolution["Results"]
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	summary, ok := results[0]["Summary"].(map[string]any)
	if !ok {
		t.Fatalf("Summary = %#v, want object", results[0]["Summary"])
	}
	if got := summary["MetadataRef"]; got != "tvdb:370070" {
		t.Fatalf("MetadataRef = %v, want tvdb:370070", got)
	}
	evidence, ok := results[0]["Evidence"].([]any)
	if !ok || len(evidence) != 1 {
		t.Fatalf("Evidence = %#v, want one entry", results[0]["Evidence"])
	}
	firstEvidence, ok := evidence[0].(map[string]any)
	if !ok {
		t.Fatalf("Evidence[0] = %#v, want object", evidence[0])
	}
	if got := firstEvidence["Term"]; got != "honzuki" {
		t.Fatalf("Evidence[0].Term = %v, want honzuki", got)
	}
	if _, ok := firstEvidence["Summary"]; ok {
		t.Fatal("Evidence[0].Summary present, want omitted")
	}
	if _, ok := firstEvidence["MetadataRef"]; ok {
		t.Fatal("Evidence[0].MetadataRef present, want omitted")
	}
}

func TestSyncCommandInitializesAndWritesMetadata(t *testing.T) {
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
		"sync",
		"--yes",
		"--tvdb-base-url", server.URL,
		"--metadata-ref", "tvdb:370070",
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
	if got := series["metadataRef"]; got != "tvdb:370070" {
		t.Fatalf("metadataRef = %v, want tvdb:370070", got)
	}
	if _, ok := series["filesystemTitle"]; ok {
		t.Fatal("filesystemTitle present, want derived from directory name")
	}
	index, err := os.ReadFile(filepath.Join(root, ".kura", "index.tsv"))
	if err != nil {
		t.Fatalf("ReadFile index.tsv: %v", err)
	}
	if got, want := string(index), "tvdb:370070\tBookworm\n"; got != want {
		t.Fatalf("index.tsv = %q, want %q", got, want)
	}
	if len(stdout.Bytes()) == 0 {
		t.Fatal("stdout is empty, want written series document")
	}
}

func TestAddCommandCreatesDirAndWritesMetadata(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"add",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}

	seriesDir := filepath.Join(root, "本好きの下剋上")
	data, err := os.ReadFile(filepath.Join(seriesDir, ".kura", "series.json"))
	if err != nil {
		t.Fatalf("ReadFile series.json: %v", err)
	}
	var series map[string]any
	if err := json.Unmarshal(data, &series); err != nil {
		t.Fatalf("unmarshal series.json: %v", err)
	}
	if got := series["metadataRef"]; got != "tvdb:370070" {
		t.Fatalf("metadataRef = %v, want tvdb:370070", got)
	}
	if got := series["preferredTitle"]; got != "本好きの下剋上" {
		t.Fatalf("preferredTitle = %v, want 本好きの下剋上", got)
	}
	if got := libraryIndexPathForRef(t, root, "tvdb:370070"); got != "本好きの下剋上" {
		t.Fatalf("index path = %q, want 本好きの下剋上", got)
	}
}

func TestAddCommandUsesDirnameOverride(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"add",
		"--tvdb-base-url", server.URL,
		"--dirname", "Bookworm",
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "Bookworm", ".kura", "series.json")); err != nil {
		t.Fatalf("Stat Bookworm series.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "本好きの下剋上")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat default dir error = %v, want not exist", err)
	}
}

func TestAddCommandRejectsExistingDirectory(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Bookworm"), 0o755); err != nil {
		t.Fatalf("Mkdir Bookworm: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"add",
		"--tvdb-base-url", server.URL,
		"--dirname", "Bookworm",
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err == nil {
		t.Fatal("run returned nil error, want existing directory error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v, want already exists", err)
	}
}

func TestAddCommandRejectsRefAlreadyTracked(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	bookwormDir := filepath.Join(root, "Bookworm")
	if err := os.Mkdir(bookwormDir, 0o755); err != nil {
		t.Fatalf("Mkdir Bookworm: %v", err)
	}
	writeSeriesJSON(t, bookwormDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm"
	}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"add",
		"--tvdb-base-url", server.URL,
		"--dirname", "Other",
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	var duplicate store.DuplicateLibraryIndexRefError
	if !errors.As(err, &duplicate) {
		t.Fatalf("error = %v, want DuplicateLibraryIndexRefError", err)
	}
}

func TestAddCommandRejectsAmbiguousQueryNonInteractive(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"add",
		"--tvdb-base-url", server.URL,
		"bookworm",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if !errors.Is(err, ui.ErrSelectionRequired) {
		t.Fatalf("error = %v, want ErrSelectionRequired", err)
	}
	if !strings.Contains(stderr.String(), "tvdb:370070") || !strings.Contains(stderr.String(), "tvdb:999999") {
		t.Fatalf("stderr = %q, want candidate refs", stderr.String())
	}
}

func TestImportCommandInitializesExistingDirectory(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Bookworm"), 0o755); err != nil {
		t.Fatalf("Mkdir Bookworm: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"import",
		"--tvdb-base-url", server.URL,
		"Bookworm",
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "Bookworm", ".kura", "series.json")); err != nil {
		t.Fatalf("Stat series.json: %v", err)
	}
	if got := libraryIndexPathForRef(t, root, "tvdb:370070"); got != "Bookworm" {
		t.Fatalf("index path = %q, want Bookworm", got)
	}
}

func TestImportCommandRejectsAlreadyTrackedDirectory(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	if err := os.Mkdir(seriesDir, 0o755); err != nil {
		t.Fatalf("Mkdir Bookworm: %v", err)
	}
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm"
	}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"import",
		"--tvdb-base-url", server.URL,
		"Bookworm",
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err == nil {
		t.Fatal("run returned nil error, want already tracked error")
	}
	if !strings.Contains(err.Error(), "already has .kura/series.json") {
		t.Fatalf("error = %v, want already tracked", err)
	}
}

func TestImportCommandRejectsMissingDirectory(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"import",
		"--tvdb-base-url", server.URL,
		"Missing",
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err == nil {
		t.Fatal("run returned nil error, want missing directory error")
	}
}

func TestImportCommandRejectsRefAlreadyTracked(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	bookwormDir := filepath.Join(root, "Bookworm")
	if err := os.Mkdir(bookwormDir, 0o755); err != nil {
		t.Fatalf("Mkdir Bookworm: %v", err)
	}
	writeSeriesJSON(t, bookwormDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm"
	}`)
	if err := os.Mkdir(filepath.Join(root, "Other"), 0o755); err != nil {
		t.Fatalf("Mkdir Other: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"import",
		"--tvdb-base-url", server.URL,
		"Other",
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	var duplicate store.DuplicateLibraryIndexRefError
	if !errors.As(err, &duplicate) {
		t.Fatalf("error = %v, want DuplicateLibraryIndexRefError", err)
	}
}

func TestScanCommandSyncsTrackedSeries(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	mediainfoCommand := newFakeMediaInfoCommand(t, root)
	if err := os.Mkdir(filepath.Join(root, "Bookworm"), 0o755); err != nil {
		t.Fatalf("Mkdir Bookworm: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{
		"import",
		"--tvdb-base-url", server.URL,
		"Bookworm",
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root)); err != nil {
		t.Fatalf("import: %v\nstderr:\n%s", err, stderr.String())
	}
	seasonDir := filepath.Join(root, "Bookworm", "Season 1")
	if err := os.Mkdir(seasonDir, 0o755); err != nil {
		t.Fatalf("Mkdir Season 1: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv"), "episode 1")
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E02 (WebRip 1080p).mkv"), "episode 2")

	stdout.Reset()
	stderr.Reset()
	err := run([]string{
		"scan",
		"--yes",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, testRunContextWithLibraryRootAndMediaInfo(&stdout, &stderr, root, mediainfoCommand))
	if err != nil {
		t.Fatalf("scan: %v\nstderr:\n%s", err, stderr.String())
	}
	series, err := store.LoadSeries(filepath.Join(root, "Bookworm"))
	if err != nil {
		t.Fatalf("LoadSeries: %v", err)
	}
	if _, ok := series.LookupEpisode(1, 1); !ok {
		t.Fatal("LookupEpisode(1, 1) = false")
	}
	if _, ok := series.LookupEpisode(1, 2); !ok {
		t.Fatal("LookupEpisode(1, 2) = false")
	}
}

func TestScanCommandFailsWhenRefNotTracked(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"scan",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err == nil {
		t.Fatal("run returned nil error, want missing tracked series error")
	}
	if !strings.Contains(err.Error(), "no tracked series") {
		t.Fatalf("error = %v, want no tracked series", err)
	}
}

func TestScanCommandUsesIndexToFindDirectory(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	mediainfoCommand := newFakeMediaInfoCommand(t, root)
	if err := os.Mkdir(filepath.Join(root, "Some Custom Name"), 0o755); err != nil {
		t.Fatalf("Mkdir Some Custom Name: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{
		"import",
		"--tvdb-base-url", server.URL,
		"Some Custom Name",
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root)); err != nil {
		t.Fatalf("import: %v\nstderr:\n%s", err, stderr.String())
	}
	seasonDir := filepath.Join(root, "Some Custom Name", "Season 1")
	if err := os.Mkdir(seasonDir, 0o755); err != nil {
		t.Fatalf("Mkdir Season 1: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "Some Custom Name - S01E01 (WebRip 1080p).mkv"), "episode")

	stdout.Reset()
	stderr.Reset()
	err := run([]string{
		"scan",
		"--yes",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, testRunContextWithLibraryRootAndMediaInfo(&stdout, &stderr, root, mediainfoCommand))
	if err != nil {
		t.Fatalf("scan: %v\nstderr:\n%s", err, stderr.String())
	}
	series, err := store.LoadSeries(filepath.Join(root, "Some Custom Name"))
	if err != nil {
		t.Fatalf("LoadSeries: %v", err)
	}
	if _, ok := series.LookupEpisode(1, 1); !ok {
		t.Fatal("LookupEpisode(1, 1) = false")
	}
}

func TestSyncCommandRejectsMetadataRefTrackedElsewhere(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	bookwormDir := filepath.Join(root, "Bookworm")
	if err := os.MkdirAll(bookwormDir, 0o755); err != nil {
		t.Fatalf("MkdirAll Bookworm: %v", err)
	}
	writeSeriesJSON(t, bookwormDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm"
	}`)
	otherDir := filepath.Join(root, "Other")
	if err := os.Mkdir(otherDir, 0o755); err != nil {
		t.Fatalf("Mkdir Other: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"sync",
		"--yes",
		"--tvdb-base-url", server.URL,
		"--metadata-ref", "tvdb:370070",
		"Other",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err == nil {
		t.Fatal("run returned nil error, want duplicate metadata ref error")
	}
	if !strings.Contains(err.Error(), "already tracked at") || !strings.Contains(err.Error(), "Bookworm") {
		t.Fatalf("error = %v, want duplicate ref at Bookworm", err)
	}
}

func TestSyncCommandWritesSummaryAndMetadata(t *testing.T) {
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
		"sync",
		"--yes",
		"--tvdb-base-url", server.URL,
		"--metadata-ref", "tvdb:370070",
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

func TestSyncCommandDoesNotPromptWhenNothingChanged(t *testing.T) {
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
		"metadataRef": "tvdb:370070",
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

func TestReconcileCommandPrintsDryRunJSON(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Long Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "old episode.mkv"), "episode")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
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

func TestReconcileCommandDoesNotPromptWhenNothingChanged(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv"), "episode")
	writeSeriesJSON(t, seriesDir, `{
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

func TestReconcileCommandDoesNotRequireTVDBKey(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll series: %v", err)
	}
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm"
	}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rt := runContext{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(key string) string {
			if key == "KURA_LIBRARY_ROOT" {
				return root
			}
			return ""
		},
	}
	err := run([]string{
		"reconcile",
		"--dry-run",
		"--json",
		"Bookworm",
	}, rt)
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
}

func TestMetaSearchReportsMissingTVDBKey(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rt := runContext{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(string) string { return "" },
	}
	err := run([]string{"meta", "search", "honzuki"}, rt)
	if err == nil {
		t.Fatal("run: nil error, want missing-key failure")
	}
	if !strings.Contains(err.Error(), "KURA_TVDB_KEY") {
		t.Fatalf("error = %v, want mention of KURA_TVDB_KEY", err)
	}
}

func TestStageCommandWritesStagedJSON(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	mediainfoCommand := newFakeMediaInfoCommand(t, root)
	seriesDir := filepath.Join(root, "Bookworm")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll series: %v", err)
	}
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm"
	}`)
	stageDir := t.TempDir()
	mediaPath := filepath.Join(stageDir, "Bookworm - S01E01 (WebRip).mkv")
	companionPath := filepath.Join(stageDir, "Bookworm - S01E01 (WebRip).en.ass")
	writeFile(t, mediaPath, "episode")
	writeFile(t, companionPath, "subtitle")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"stage",
		"--season", "1",
		"--number", "1",
		"--tvdb-base-url", server.URL,
		"--companion", companionPath,
		"Bookworm",
		mediaPath,
	}, testRunContextWithLibraryRootAndMediaInfo(&stdout, &stderr, root, mediainfoCommand))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	if got := result["series"]; got != "Bookworm" {
		t.Fatalf("series = %v, want Bookworm", got)
	}
	data, err := os.ReadFile(filepath.Join(seriesDir, ".kura", "staged.json"))
	if err != nil {
		t.Fatalf("ReadFile staged.json: %v", err)
	}
	var staged map[string]any
	if err := json.Unmarshal(data, &staged); err != nil {
		t.Fatalf("unmarshal staged: %v", err)
	}
	entry := staged["entries"].([]any)[0].(map[string]any)
	media := entry["media"].(map[string]any)
	if got := media["path"]; got != mediaPath {
		t.Fatalf("media.path = %v, want %s", got, mediaPath)
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

func libraryIndexPathForRef(t *testing.T, rootPath string, metadataRef string) string {
	t.Helper()
	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	index, err := store.LoadLibraryIndex(root)
	if err != nil {
		t.Fatalf("LoadLibraryIndex: %v", err)
	}
	ref, err := domain.ParseMetadataRef(metadataRef)
	if err != nil {
		t.Fatalf("ParseMetadataRef: %v", err)
	}
	path, ok, err := index.Get(ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatalf("Get(%s) = false", metadataRef)
	}
	return path.String()
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
				{
					"id":             999999,
					"tvdb_id":        "999999",
					"name":           "Bookworm Extra",
					"type":           "series",
					"year":           2020,
					"first_air_time": "2020-01-01",
					"genres":         nil,
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
					{
						"id":             1002,
						"name":           "Life Improvements and Slates",
						"aired":          "2019-10-10",
						"number":         2,
						"seasonNumber":   1,
						"absoluteNumber": 2,
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

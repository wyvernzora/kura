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

	librarypkg "github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/refs"
	seriespkg "github.com/wyvernzora/kura/internal/series"
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
	if _, ok := series["preferredTitle"]; ok {
		t.Fatal("preferredTitle present; provider display titles should not be persisted")
	}
	episodes, ok := series["episodes"].(map[string]any)
	if !ok || len(episodes) == 0 {
		t.Fatalf("episodes = %#v, want persisted spine", series["episodes"])
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
		"episodes": {}
	}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"add",
		"--tvdb-base-url", server.URL,
		"--dirname", "Other",
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	var duplicate seriespkg.MetadataRefConflictError
	if !errors.As(err, &duplicate) {
		t.Fatalf("error = %v, want MetadataRefConflictError", err)
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

func TestImportCommandUsesDirnameAsSearchTerm(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Honzuki"), 0o755); err != nil {
		t.Fatalf("Mkdir Honzuki: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"import",
		"--tvdb-base-url", server.URL,
		"Honzuki",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "Honzuki", ".kura", "series.json")); err != nil {
		t.Fatalf("Stat series.json: %v", err)
	}
	if got := libraryIndexPathForRef(t, root, "tvdb:370070"); got != "Honzuki" {
		t.Fatalf("index path = %q, want Honzuki", got)
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
		"episodes": {}
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

func TestImportCommandForceReplacesSeriesJSONAndPreservesKuraSiblings(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:999999",
		"episodes": {}
	}`)
	trashMeta := filepath.Join(seriesDir, ".kura", "trash", "old", "meta.json")
	logFile := filepath.Join(seriesDir, ".kura", "logs", "old.jsonl")
	if err := os.MkdirAll(filepath.Dir(trashMeta), 0o755); err != nil {
		t.Fatalf("MkdirAll trash: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		t.Fatalf("MkdirAll logs: %v", err)
	}
	writeFile(t, trashMeta, "{}")
	writeFile(t, logFile, "{}\n")
	writeLibraryIndex(t, root, map[string]string{"tvdb:999999": "Bookworm"})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"import",
		"--force",
		"--tvdb-base-url", server.URL,
		"Bookworm",
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	doc, err := loadSeriesDocument(t, seriesDir)
	if err != nil {
		t.Fatalf("load series document: %v", err)
	}
	if got := doc["metadataRef"]; got != "tvdb:370070" {
		t.Fatalf("metadataRef = %v, want tvdb:370070", got)
	}
	if _, err := os.Stat(trashMeta); err != nil {
		t.Fatalf("trash meta was not preserved: %v", err)
	}
	if _, err := os.Stat(logFile); err != nil {
		t.Fatalf("log file was not preserved: %v", err)
	}
	if libraryIndexHasRef(t, root, "tvdb:999999") {
		t.Fatal("old index ref tvdb:999999 still exists")
	}
	if got := libraryIndexPathForRef(t, root, "tvdb:370070"); got != "Bookworm" {
		t.Fatalf("index path = %q, want Bookworm", got)
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
		"episodes": {}
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
	var duplicate seriespkg.MetadataRefConflictError
	if !errors.As(err, &duplicate) {
		t.Fatalf("error = %v, want MetadataRefConflictError", err)
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
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, testRunContextWithLibraryRootAndMediaInfo(&stdout, &stderr, root, mediainfoCommand))
	if err != nil {
		t.Fatalf("scan: %v\nstderr:\n%s", err, stderr.String())
	}
	series, err := loadSeriesDocument(t, filepath.Join(root, "Bookworm"))
	if err != nil {
		t.Fatalf("load series document: %v", err)
	}
	episodes := series["episodes"].(map[string]any)
	if _, ok := episodes["S01E0001"]; !ok {
		t.Fatal("episode S01E0001 missing")
	}
	if _, ok := episodes["S01E0002"]; !ok {
		t.Fatal("episode S01E0002 missing")
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
	var notIndexed seriespkg.MetadataRefNotIndexedError
	if !errors.As(err, &notIndexed) {
		t.Fatalf("error = %v, want MetadataRefNotIndexedError", err)
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
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, testRunContextWithLibraryRootAndMediaInfo(&stdout, &stderr, root, mediainfoCommand))
	if err != nil {
		t.Fatalf("scan: %v\nstderr:\n%s", err, stderr.String())
	}
	series, err := loadSeriesDocument(t, filepath.Join(root, "Some Custom Name"))
	if err != nil {
		t.Fatalf("load series document: %v", err)
	}
	episodes := series["episodes"].(map[string]any)
	if _, ok := episodes["S01E0001"]; !ok {
		t.Fatal("episode S01E0001 missing")
	}
}

func TestShowCommandPrintsTrackedSeriesTable(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "episode-1.mkv"), "episode 1")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"lastScanned": "2026-04-20T03:00:00Z",
		"episodes": {
			"S01E0001": {
				"airDate": "2019-10-03",
				"active": {
					"path": "Season 1/episode-1.mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			},
			"S01E0002": {"airDate": "2019-10-10"}
		}
	}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"show",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"MetadataRef: tvdb:370070",
		"Root: " + seriesDir,
		"LastScanned: 2026-04-20T03:00:00Z",
		"Title: Bookworm",
		"SEASON 1",
		"present",
		"missing",
		"WebRip",
		"1080p",
		"Season 1/episode-1.mkv",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestShowCommandPrintsJSON(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	if err := os.MkdirAll(filepath.Join(seriesDir, "Season 1"), 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeFile(t, filepath.Join(seriesDir, "Season 1", "episode-1.mkv"), "episode 1")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"lastScanned": "2026-04-20T03:00:00Z",
		"episodes": {
			"S01E0001": {
				"airDate": "2019-10-03",
				"active": {
					"path": "Season 1/episode-1.mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			},
			"S01E0002": {"airDate": "2019-10-10"}
		}
	}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"show",
		"--json",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	if got := result["lastScanned"]; got != "2026-04-20T03:00:00Z" {
		t.Fatalf("lastScanned = %v, want 2026-04-20T03:00:00Z", got)
	}
	seasons := result["seasons"].([]any)
	episodes := seasons[0].(map[string]any)["episodes"].([]any)
	if got := episodes[0].(map[string]any)["status"]; got != "present" {
		t.Fatalf("episode 1 status = %v, want present", got)
	}
	if got := episodes[1].(map[string]any)["status"]; got != "missing" {
		t.Fatalf("episode 2 status = %v, want missing", got)
	}
}

func TestFindCommandIsRemoved(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"find", "tvdb:370070"}, testRunContext(&stdout, &stderr))
	if err == nil {
		t.Fatal("find command succeeded, want removed command error")
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
		"episodes": {
			"S01E0001": {
				"airDate": "2019-10-03",
				"active": {
					"path": "Season 1/old episode.mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"codec": "HEVC",
					"size": 7,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			}
		}
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
	if changes := plan["changes"].([]any); len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
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
		"episodes": {
			"S01E0001": {
				"airDate": "2019-10-03",
				"active": {
					"path": "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"codec": "HEVC",
					"size": 7,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			}
		}
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

func TestReconcileCommandReportsMissingTVDBKey(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll series: %v", err)
	}
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {}
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
	if !errors.Is(err, librarypkg.ErrMissingTVDBKey) {
		t.Fatalf("run error = %v, want ErrMissingTVDBKey", err)
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

func TestStageCommandWritesStagedEpisode(t *testing.T) {
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
		"episodes": {
			"S01E0001": {"airDate": "2019-10-03"}
		}
	}`)
	stageDir := filepath.Join(seriesDir, "incoming")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatalf("MkdirAll incoming: %v", err)
	}
	mediaRelPath := filepath.ToSlash(filepath.Join("incoming", "Bookworm - S01E01 (WebRip).mkv"))
	companionRelPath := filepath.ToSlash(filepath.Join("incoming", "Bookworm - S01E01 (WebRip).en.ass"))
	mediaPath := filepath.Join(seriesDir, filepath.FromSlash(mediaRelPath))
	companionPath := filepath.Join(seriesDir, filepath.FromSlash(companionRelPath))
	writeFile(t, mediaPath, "episode")
	writeFile(t, companionPath, "subtitle")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"stage",
		"--episode", "S01E01",
		"--tvdb-base-url", server.URL,
		"--companion", companionRelPath,
		"tvdb:370070",
		mediaRelPath,
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
	data, err := os.ReadFile(filepath.Join(seriesDir, ".kura", "series.json"))
	if err != nil {
		t.Fatalf("ReadFile series.json: %v", err)
	}
	var series map[string]any
	if err := json.Unmarshal(data, &series); err != nil {
		t.Fatalf("unmarshal series: %v", err)
	}
	episodes := series["episodes"].(map[string]any)
	entry := episodes["S01E0001"].(map[string]any)
	media := entry["staged"].(map[string]any)
	if got := media["path"]; got != mediaPath {
		t.Fatalf("media.path = %v, want %s", got, mediaPath)
	}
	companions := media["companions"].([]any)
	companion := companions[0].(map[string]any)
	if got := companion["path"]; got != companionPath {
		t.Fatalf("companion.path = %v, want %s", got, companionPath)
	}
}

func TestResetCommandClearsStagedEpisode(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	stagedPath := filepath.Join(t.TempDir(), "Bookworm - S01E01 (WebRip).mkv")
	writeFile(t, stagedPath, "episode")
	writeSeriesJSON(t, seriesDir, fmt.Sprintf(`{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S01E0001": {
				"airDate": "2019-10-03",
				"staged": {
					"path": %q,
					"source": "webrip",
					"resolution": "1920x1080",
					"codec": "HEVC",
					"size": 7,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			}
		}
	}`, stagedPath))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"reset",
		"--episode", "S01E01",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	if got := result["applied"]; got != true {
		t.Fatalf("applied = %v, want true", got)
	}
	data, err := os.ReadFile(filepath.Join(seriesDir, ".kura", "series.json"))
	if err != nil {
		t.Fatalf("ReadFile series.json: %v", err)
	}
	var series map[string]any
	if err := json.Unmarshal(data, &series); err != nil {
		t.Fatalf("unmarshal series: %v", err)
	}
	episodes := series["episodes"].(map[string]any)
	entry := episodes["S01E0001"].(map[string]any)
	if _, ok := entry["staged"]; ok {
		t.Fatalf("staged entry = %#v, want removed", entry["staged"])
	}
}

func TestResetCommandRequiresEpisode(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"reset",
		"tvdb:370070",
	}, testRunContext(&stdout, &stderr))
	if err == nil {
		t.Fatal("run returned nil, want missing --episode error")
	}
}

func TestParseStageEpisodeAcceptsMarkerAndStorageRef(t *testing.T) {
	cases := map[string]string{
		"S01E01":   "S01E0001",
		"S01E0001": "S01E0001",
		"S00E06":   "S00E0006",
	}
	for input, want := range cases {
		got, err := parseStageEpisode(input)
		if err != nil {
			t.Fatalf("parseStageEpisode(%q): %v", input, err)
		}
		if got.String() != want {
			t.Fatalf("parseStageEpisode(%q) = %s, want %s", input, got, want)
		}
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
	root, err := librarypkg.ParseRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseRoot: %v", err)
	}
	idx, err := librarypkg.LoadIndex(root)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	path, ok, err := idx.Get(refs.Metadata(metadataRef))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatalf("Get(%s) = false", metadataRef)
	}
	return path.String()
}

func libraryIndexHasRef(t *testing.T, rootPath string, metadataRef string) bool {
	t.Helper()
	root, err := librarypkg.ParseRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseRoot: %v", err)
	}
	idx, err := librarypkg.LoadIndex(root)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	_, ok, err := idx.Get(refs.Metadata(metadataRef))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return ok
}

func writeLibraryIndex(t *testing.T, rootPath string, entries map[string]string) {
	t.Helper()
	root, err := librarypkg.ParseRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseRoot: %v", err)
	}
	idx := librarypkg.NewIndex(root)
	for metadataRef, seriesRef := range entries {
		ref, err := refs.ParseSeries(seriesRef)
		if err != nil {
			t.Fatalf("ParseSeries: %v", err)
		}
		if err := idx.Put(refs.Metadata(metadataRef), ref); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
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
		results := []map[string]any{
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
		}
		if r.URL.Query().Get("query") == "Honzuki" {
			results = results[:1]
		}
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data":   results,
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

func loadSeriesDocument(t *testing.T, seriesDir string) (map[string]any, error) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(seriesDir, ".kura", "series.json"))
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

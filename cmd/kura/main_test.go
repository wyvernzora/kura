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

	"github.com/oklog/ulid/v2"
	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/trashfile"
	"github.com/wyvernzora/kura/internal/workflow"
)

func TestResolveCommandPrintsJSON(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"resolve",
		"--json",
		"--tvdb-base-url", server.URL,
		"--limit", "1",
		"honzuki",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}

	var resolution map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &resolution); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	candidates, ok := resolution["candidates"].([]any)
	if !ok {
		t.Fatalf("candidates = %#v, want array", resolution["candidates"])
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
	first, ok := candidates[0].(map[string]any)
	if !ok {
		t.Fatalf("candidates[0] = %#v, want object", candidates[0])
	}
	if got := first["ref"]; got != "tvdb:370070" {
		t.Fatalf("ref = %v, want tvdb:370070", got)
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
	if got := series["canonicalTitle"]; got != "Ascendance of a Bookworm" {
		t.Fatalf("canonicalTitle = %v, want Ascendance of a Bookworm", got)
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
	var duplicate *workflow.MetadataRefConflictError
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
	if !errors.Is(err, clipkg.ErrAmbiguousSelector) {
		t.Fatalf("error = %v, want ErrAmbiguousSelector", err)
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
	var duplicate *workflow.MetadataRefConflictError
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
	var notIndexed *workflow.MetadataRefNotIndexedError
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
					"codec": "HEVC",
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
	for _, unwanted := range []string{"HEVC"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("stdout = %q, did not want %q in table output", output, unwanted)
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
					"codec": "HEVC",
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
	active := episodes[0].(map[string]any)["active"].(map[string]any)
	if got := active["codec"]; got != "HEVC" {
		t.Fatalf("episode 1 active codec = %v, want HEVC", got)
	}
	if got := active["size"]; got != float64(9) {
		t.Fatalf("episode 1 active size = %v, want 9", got)
	}
	if got := episodes[1].(map[string]any)["status"]; got != "missing" {
		t.Fatalf("episode 2 status = %v, want missing", got)
	}
}

func TestListCommandPrintsLibraryInventoryJSON(t *testing.T) {
	root := t.TempDir()
	writeSeriesJSON(t, filepath.Join(root, "Complete"), `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:1",
		"preferredTitle": "Complete Title",
		"canonicalTitle": "Complete Canonical",
		"episodes": {
			"S00E0001": {"airDate": "2019-01-01"},
			"S01E0001": {
				"airDate": "2019-01-01",
				"active": {
					"path": "Season 1/episode-1.mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			},
			"S02E0001": {
				"airDate": "2026-01-01",
				"staged": {
					"path": "/inbox/episode-1.mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			}
		}
	}`)
	writeSeriesJSON(t, filepath.Join(root, "Airing"), `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:2",
		"preferredTitle": "Airing Title",
		"canonicalTitle": "Airing Canonical",
		"episodes": {
			"S01E0001": {"airDate": "2099-01-01"}
		}
	}`)
	writeSeriesJSON(t, filepath.Join(root, "Incomplete"), `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:3",
		"preferredTitle": "Incomplete Title",
		"canonicalTitle": "Incomplete Canonical",
		"episodes": {
			"S01E0001": {"airDate": "2019-01-01"}
		}
	}`)
	writeSeriesJSON(t, filepath.Join(root, "Empty"), `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:4",
		"preferredTitle": "Empty Title",
		"canonicalTitle": "Empty Canonical",
		"episodes": {}
	}`)
	if err := os.Mkdir(filepath.Join(root, "Untracked"), 0o755); err != nil {
		t.Fatalf("Mkdir Untracked: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, ".hidden"), 0o755); err != nil {
		t.Fatalf("Mkdir .hidden: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Broken", ".kura"), 0o755); err != nil {
		t.Fatalf("MkdirAll Broken: %v", err)
	}
	writeFile(t, filepath.Join(root, "Broken", ".kura", "series.json"), "{")

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
	err := run([]string{"list", "--json"}, rt)
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	byTitle := map[string]map[string]any{}
	for _, row := range rows {
		byTitle[row["title"].(string)] = row
	}
	if _, ok := byTitle[".hidden"]; ok {
		t.Fatalf("hidden directory listed: %#v", rows)
	}
	for title, want := range map[string]string{
		"Airing Title":     "airing",
		"Broken":           "error",
		"Complete Title":   "complete",
		"Empty Title":      "incomplete",
		"Incomplete Title": "incomplete",
		"Untracked":        "untracked",
	} {
		row, ok := byTitle[title]
		if !ok {
			t.Fatalf("missing row %q in %#v", title, rows)
		}
		if got := row["status"]; got != want {
			t.Fatalf("%s status = %v, want %s", title, got, want)
		}
	}
	if got := byTitle["Complete Title"]["canonicalTitle"]; got != "Complete Canonical" {
		t.Fatalf("Complete canonicalTitle = %v, want Complete Canonical", got)
	}
	if got := byTitle["Complete Title"]["staged"]; got != true {
		t.Fatalf("Complete staged = %v, want true", got)
	}
	if got := byTitle["Complete Title"]["seasonCount"]; got != float64(2) {
		t.Fatalf("Complete seasonCount = %v, want 2", got)
	}
	if got := byTitle["Complete Title"]["episodeCount"]; got != float64(2) {
		t.Fatalf("Complete episodeCount = %v, want 2", got)
	}
}

func TestListCommandPrintsStagedStatusMarker(t *testing.T) {
	root := t.TempDir()
	writeSeriesJSON(t, filepath.Join(root, "Bookworm"), `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm",
		"episodes": {
			"S00E0001": {
				"airDate": "2019-01-01",
				"staged": {
					"path": "/inbox/special.mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			},
			"S01E0001": {
				"airDate": "2019-01-01",
				"active": {
					"path": "Season 1/episode-1.mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			}
		}
	}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"list"}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{"STATUS", "ID", "RESOLUTION", "SOURCE", "TITLE", "SEASONS", "EPISODES", "complete*", "Bookworm"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestListCommandFiltersByStatus(t *testing.T) {
	root := t.TempDir()
	writeSeriesJSON(t, filepath.Join(root, "Complete"), `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:1",
		"preferredTitle": "Complete Title",
		"canonicalTitle": "Complete Canonical",
		"episodes": {
			"S01E0001": {
				"airDate": "2019-01-01",
				"staged": {
					"path": "/inbox/episode-1.mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			}
		}
	}`)
	writeSeriesJSON(t, filepath.Join(root, "Incomplete"), `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:2",
		"preferredTitle": "Incomplete Title",
		"canonicalTitle": "Incomplete Canonical",
		"episodes": {
			"S01E0001": {"airDate": "2019-01-01"}
		}
	}`)
	if err := os.Mkdir(filepath.Join(root, "Untracked"), 0o755); err != nil {
		t.Fatalf("Mkdir Untracked: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"list",
		"--json",
		"--status", "incomplete",
		"--status", "untracked",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2: %#v", len(rows), rows)
	}
	titles := map[string]bool{}
	for _, row := range rows {
		titles[row["title"].(string)] = true
	}
	if titles["Complete Title"] {
		t.Fatalf("filtered rows include staged-complete series: %#v", rows)
	}
	if !titles["Incomplete Title"] || !titles["Untracked"] {
		t.Fatalf("filtered titles = %#v, want Incomplete Title and Untracked", titles)
	}
}

func TestListCommandRejectsInvalidStatusFilter(t *testing.T) {
	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"list",
		"--status", "missing",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err == nil {
		t.Fatal("run returned nil error, want invalid status error")
	}
	if !strings.Contains(err.Error(), "invalid list status") {
		t.Fatalf("error = %v, want invalid list status", err)
	}
}

func TestReindexCommandRebuildsLibraryIndex(t *testing.T) {
	root := t.TempDir()
	writeSeriesJSON(t, filepath.Join(root, "Bookworm"), `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm",
		"episodes": {}
	}`)
	writeSeriesJSON(t, filepath.Join(root, "Other"), `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:111111",
		"preferredTitle": "Other",
		"canonicalTitle": "Other",
		"episodes": {}
	}`)
	writeLibraryIndex(t, root, map[string]string{
		"tvdb:999999": "Bookworm",
	})

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
	err := run([]string{"reindex"}, rt)
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	if libraryIndexHasRef(t, root, "tvdb:999999") {
		t.Fatal("stale index ref tvdb:999999 still exists")
	}
	if got := libraryIndexPathForRef(t, root, "tvdb:370070"); got != "Bookworm" {
		t.Fatalf("Bookworm index path = %q, want Bookworm", got)
	}
	if got := libraryIndexPathForRef(t, root, "tvdb:111111"); got != "Other" {
		t.Fatalf("Other index path = %q, want Other", got)
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

func TestCombinedReconcileCommandIsRemoved(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"reconcile", "tvdb:370070"}, testRunContext(&stdout, &stderr))
	if err == nil {
		t.Fatal("combined reconcile command succeeded, want removed command error")
	}
}

func TestReconcilePlanCommandWritesPlanJSON(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

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
		"plan",
		"--json",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	if output["token"] == "" {
		t.Fatalf("token = %v, want non-empty token", output["token"])
	}
	plan := output["plan"].(map[string]any)
	if changes := plan["changes"].([]any); len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	matches, err := filepath.Glob(filepath.Join(seriesDir, ".kura", "reconcile", "*.jsonl"))
	if err != nil {
		t.Fatalf("glob reconcile plans: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("plan files = %d, want 1", len(matches))
	}
}

func TestReconcilePlanCommandWritesNoPlanWhenNothingChanged(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

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
		"plan",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	matches, err := filepath.Glob(filepath.Join(seriesDir, ".kura", "reconcile", "*.jsonl"))
	if err != nil {
		t.Fatalf("glob reconcile plans: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("plan files = %d, want 0", len(matches))
	}
}

func TestReconcileApplyCommandAppliesPlanToken(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
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
	rt := testRunContextWithLibraryRoot(&stdout, &stderr, root)
	err := run([]string{
		"reconcile",
		"plan",
		"--json",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, rt)
	if err != nil {
		t.Fatalf("plan: %v\nstderr:\n%s", err, stderr.String())
	}
	var planOutput map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &planOutput); err != nil {
		t.Fatalf("unmarshal plan stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	token := planOutput["token"].(string)

	stdout.Reset()
	stderr.Reset()
	err = run([]string{
		"reconcile",
		"apply",
		"--json",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
		token,
	}, rt)
	if err != nil {
		t.Fatalf("apply: %v\nstderr:\n%s", err, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal apply stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	if got := int(result["appliedMoves"].(float64)); got != 1 {
		t.Fatalf("appliedMoves = %d, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv")); err != nil {
		t.Fatalf("stat reconciled file: %v", err)
	}
	planPath := filepath.Join(seriesDir, ".kura", "reconcile", token+".jsonl")
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan log: %v", err)
	}
	if lines := strings.Split(strings.TrimSpace(string(data)), "\n"); len(lines) != 3 {
		t.Fatalf("plan log lines = %d, want 3\n%s", len(lines), string(data))
	}
	if !strings.Contains(string(data), `"type":"event"`) || !strings.Contains(string(data), `"status":"success"`) {
		t.Fatalf("plan log missing move/result records:\n%s", string(data))
	}
}

func TestReconcilePlanIsIdempotent(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
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

	var stdout, stderr bytes.Buffer
	rt := testRunContextWithLibraryRoot(&stdout, &stderr, root)
	planArgs := []string{
		"reconcile", "plan", "--json",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}
	if err := run(planArgs, rt); err != nil {
		t.Fatalf("first plan: %v\nstderr:\n%s", err, stderr.String())
	}
	var first map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &first); err != nil {
		t.Fatalf("unmarshal first: %v\n%s", err, stdout.String())
	}
	tokenA := first["token"].(string)
	createdA := first["createdAt"].(string)

	planPath := filepath.Join(seriesDir, ".kura", "reconcile", tokenA+".jsonl")
	infoBefore, err := os.Stat(planPath)
	if err != nil {
		t.Fatalf("stat plan: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if err := run(planArgs, rt); err != nil {
		t.Fatalf("second plan: %v\nstderr:\n%s", err, stderr.String())
	}
	var second map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &second); err != nil {
		t.Fatalf("unmarshal second: %v\n%s", err, stdout.String())
	}
	if got := second["token"].(string); got != tokenA {
		t.Fatalf("second token = %q, want %q (snapshot-derived)", got, tokenA)
	}
	if got := second["createdAt"].(string); got != createdA {
		t.Fatalf("second createdAt = %q, want %q (existing record returned, not rewritten)", got, createdA)
	}
	infoAfter, err := os.Stat(planPath)
	if err != nil {
		t.Fatalf("stat plan after: %v", err)
	}
	if !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Fatalf("plan file mtime changed: before=%v after=%v (idempotency expects no rewrite)", infoBefore.ModTime(), infoAfter.ModTime())
	}
}

func TestReconcileApplyHandlesSelfRefresh(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	mediainfoCommand := newFakeMediaInfoCommand(t, root)
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	canonicalRel := "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv"
	canonicalAbs := filepath.Join(seriesDir, filepath.FromSlash(canonicalRel))
	writeFile(t, canonicalAbs, "episode")
	writeSeriesJSON(t, seriesDir, fmt.Sprintf(`{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S01E0001": {
				"airDate": "2019-10-03",
				"active": {
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
	}`, canonicalRel))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rt := testRunContextWithLibraryRootAndMediaInfo(&stdout, &stderr, root, mediainfoCommand)
	err := run([]string{
		"stage",
		"--episode", "S01E01",
		"--source", "WebRip",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
		canonicalRel,
	}, rt)
	if err != nil {
		t.Fatalf("stage: %v\nstderr:\n%s", err, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	err = run([]string{
		"reconcile",
		"plan",
		"--json",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, rt)
	if err != nil {
		t.Fatalf("plan: %v\nstderr:\n%s", err, stderr.String())
	}
	var planOutput map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &planOutput); err != nil {
		t.Fatalf("unmarshal plan: %v\nstdout:\n%s", err, stdout.String())
	}
	token := planOutput["token"].(string)
	plan := planOutput["plan"].(map[string]any)
	changes := plan["changes"].([]any)
	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	change := changes[0].(map[string]any)
	if got := change["kind"]; got != "add" {
		t.Fatalf("kind = %v, want add (self-refresh skips trash)", got)
	}
	if _, ok := change["replaced"]; ok {
		t.Fatalf("replaced field present, want absent for self-refresh: %#v", change)
	}

	stdout.Reset()
	stderr.Reset()
	err = run([]string{
		"reconcile",
		"apply",
		"--json",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
		token,
	}, rt)
	if err != nil {
		t.Fatalf("apply: %v\nstderr:\n%s", err, stderr.String())
	}
	if _, err := os.Stat(canonicalAbs); err != nil {
		t.Fatalf("canonical file missing after self-refresh apply: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(seriesDir, ".kura", "trash")); err == nil && len(entries) > 0 {
		t.Fatalf("trash dir non-empty after self-refresh: %v entries", len(entries))
	}
}

func TestReconcileApplySelfRefreshRenamesInPlace(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	mediainfoCommand := newFakeMediaInfoCommand(t, root)
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	originalRel := "Season 1/[LoliHouse] Bookworm - 01 [WebRip 1080p].mkv"
	originalAbs := filepath.Join(seriesDir, filepath.FromSlash(originalRel))
	writeFile(t, originalAbs, "episode")
	writeSeriesJSON(t, seriesDir, fmt.Sprintf(`{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S01E0001": {
				"airDate": "2019-10-03",
				"active": {
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
	}`, originalRel))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rt := testRunContextWithLibraryRootAndMediaInfo(&stdout, &stderr, root, mediainfoCommand)
	err := run([]string{
		"stage",
		"--episode", "S01E01",
		"--source", "BDRip",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
		originalRel,
	}, rt)
	if err != nil {
		t.Fatalf("stage: %v\nstderr:\n%s", err, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	err = run([]string{
		"reconcile",
		"plan",
		"--json",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
	}, rt)
	if err != nil {
		t.Fatalf("plan: %v\nstderr:\n%s", err, stderr.String())
	}
	var planOutput map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &planOutput); err != nil {
		t.Fatalf("unmarshal plan: %v\nstdout:\n%s", err, stdout.String())
	}
	token := planOutput["token"].(string)
	plan := planOutput["plan"].(map[string]any)
	changes := plan["changes"].([]any)
	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	change := changes[0].(map[string]any)
	if got := change["kind"]; got != "add" {
		t.Fatalf("kind = %v, want add", got)
	}
	if _, ok := change["replaced"]; ok {
		t.Fatalf("replaced field present, want absent: %#v", change)
	}

	stdout.Reset()
	stderr.Reset()
	err = run([]string{
		"reconcile",
		"apply",
		"--json",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
		token,
	}, rt)
	if err != nil {
		t.Fatalf("apply: %v\nstderr:\n%s", err, stderr.String())
	}
	canonicalAbs := filepath.Join(seasonDir, "Bookworm - S01E01 (BluRay 1080p).mkv")
	if _, err := os.Stat(canonicalAbs); err != nil {
		t.Fatalf("canonical (BluRay) file missing after rename: %v", err)
	}
	if _, err := os.Stat(originalAbs); !os.IsNotExist(err) {
		t.Fatalf("original LoliHouse file still present after rename: err=%v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(seriesDir, ".kura", "trash")); err == nil && len(entries) > 0 {
		t.Fatalf("trash dir non-empty after self-refresh rename: %d entries", len(entries))
	}
}

func TestStageAllowsSamePathRefreshWithoutReplace(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	mediainfoCommand := newFakeMediaInfoCommand(t, root)
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	canonicalRel := "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv"
	canonicalAbs := filepath.Join(seriesDir, filepath.FromSlash(canonicalRel))
	writeFile(t, canonicalAbs, "episode")
	writeSeriesJSON(t, seriesDir, fmt.Sprintf(`{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S01E0001": {
				"airDate": "2019-10-03",
				"active": {
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
	}`, canonicalRel))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rt := testRunContextWithLibraryRootAndMediaInfo(&stdout, &stderr, root, mediainfoCommand)
	err := run([]string{
		"stage",
		"--episode", "S01E01",
		"--source", "WebRip",
		"--tvdb-base-url", server.URL,
		"tvdb:370070",
		canonicalRel,
	}, rt)
	if err != nil {
		t.Fatalf("same-path stage without --replace: %v\nstderr:\n%s", err, stderr.String())
	}
}

func TestReconcilePlanCommandReportsMissingTVDBKey(t *testing.T) {
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
		"plan",
		"--json",
		"Bookworm",
	}, rt)
	if err == nil || !strings.Contains(err.Error(), "KURA_TVDB_KEY") {
		t.Fatalf("run error = %v, want missing-key failure", err)
	}
}

func TestResolveReportsMissingTVDBKey(t *testing.T) {
	root := t.TempDir()
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
	err := run([]string{"resolve", "honzuki"}, rt)
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
	if got, _ := result["replaced"].(bool); got {
		t.Fatalf("replaced = %v, want false (slot was empty)", got)
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
	if _, ok := result["record"]; !ok {
		t.Fatalf("expected dropped record in result, got: %v", result)
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

func TestResetCommandClearsAllStagedEpisodes(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	firstPath := filepath.Join(t.TempDir(), "Bookworm - S01E01 (WebRip).mkv")
	secondPath := filepath.Join(t.TempDir(), "Bookworm - S01E02 (WebRip).mkv")
	writeFile(t, firstPath, "episode 1")
	writeFile(t, secondPath, "episode 2")
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
			},
			"S01E0002": {
				"airDate": "2019-10-10",
				"staged": {
					"path": %q,
					"source": "webrip",
					"resolution": "1920x1080",
					"codec": "HEVC",
					"size": 8,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			}
		}
	}`, firstPath, secondPath))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"reset",
		"--all",
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
	records := result["records"].([]any)
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	data, err := os.ReadFile(filepath.Join(seriesDir, ".kura", "series.json"))
	if err != nil {
		t.Fatalf("ReadFile series.json: %v", err)
	}
	var series map[string]any
	if err := json.Unmarshal(data, &series); err != nil {
		t.Fatalf("unmarshal series: %v", err)
	}
	for key, entry := range series["episodes"].(map[string]any) {
		if _, ok := entry.(map[string]any)["staged"]; ok {
			t.Fatalf("%s staged entry = %#v, want removed", key, entry)
		}
	}
}

func TestResetCommandRequiresEpisodeOrAll(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"reset",
		"tvdb:370070",
	}, testRunContext(&stdout, &stderr))
	if err == nil {
		t.Fatal("run returned nil, want missing --episode or --all error")
	}
}

func TestResetCommandRejectsEpisodeAndAll(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"reset",
		"--episode", "S01E01",
		"--all",
		"tvdb:370070",
	}, testRunContext(&stdout, &stderr))
	if err == nil {
		t.Fatal("run returned nil, want mutually exclusive reset mode error")
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
	idx, err := indexfile.Load(rootPath)
	if err != nil {
		t.Fatalf("indexfile.Load: %v", err)
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
	idx, err := indexfile.Load(rootPath)
	if err != nil {
		t.Fatalf("indexfile.Load: %v", err)
	}
	_, ok, err := idx.Get(refs.Metadata(metadataRef))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return ok
}

func writeLibraryIndex(t *testing.T, rootPath string, entries map[string]string) {
	t.Helper()
	idx := indexfile.New(rootPath)
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

func writeTrashEntry(t *testing.T, root string, ref refs.Series, episode refs.Episode, trashedAt time.Time, mediaName string, body string) ulid.ULID {
	t.Helper()
	id := ulid.MustNew(ulid.Timestamp(trashedAt), ulid.DefaultEntropy())
	meta := trashfile.Meta{
		ID:        id,
		Episode:   episode,
		TrashedAt: trashedAt,
		Record: trashfile.Record{
			Path:       fmt.Sprintf("Season %d/%s", episode.Season(), mediaName),
			Source:     "webrip",
			Resolution: "1920x1080",
			Codec:      "HEVC",
			Size:       int64(len(body)),
			MTime:      trashedAt,
			Companions: []trashfile.Companion{},
		},
	}
	if err := trashfile.Write(root, ref, meta); err != nil {
		t.Fatalf("trashfile.Write: %v", err)
	}
	mediaPath := filepath.Join(paths.TrashEntry(root, ref, id.String()), mediaName)
	if err := os.WriteFile(mediaPath, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile media: %v", err)
	}
	return id
}

func setupTrashSeries(t *testing.T, root, name, metadataID string) refs.Series {
	t.Helper()
	seriesDir := filepath.Join(root, name)
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll series: %v", err)
	}
	writeSeriesJSON(t, seriesDir, fmt.Sprintf(`{
		"schemaVersion": 1,
		"metadataRef": "tvdb:%s",
		"episodes": {}
	}`, metadataID))
	ref, err := refs.ParseSeries(name)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestTrashListPerSeriesJSON(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()
	root := t.TempDir()
	ref := setupTrashSeries(t, root, "Bookworm", "370070")
	episode, _ := refs.NewEpisode(1, 3)
	id := writeTrashEntry(t, root, ref, episode,
		time.Date(2026, 4, 20, 3, 0, 0, 0, time.UTC),
		"old episode.mkv", "episode-body")

	var stdout, stderr bytes.Buffer
	rt := testRunContextWithLibraryRoot(&stdout, &stderr, root)
	if err := run([]string{"trash", "list", "--json", "--tvdb-base-url", server.URL, "tvdb:370070"}, rt); err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}

	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	series := result["series"].([]any)
	if len(series) != 1 {
		t.Fatalf("len(series) = %d, want 1", len(series))
	}
	first := series[0].(map[string]any)
	entries := first["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].(map[string]any)["id"] != id.String() {
		t.Fatalf("id = %v, want %s", entries[0].(map[string]any)["id"], id.String())
	}
}

func TestTrashListAllJSON(t *testing.T) {
	root := t.TempDir()
	bookwormRef := setupTrashSeries(t, root, "Bookworm", "370070")
	otonariRef := setupTrashSeries(t, root, "Otonari", "999999")
	episode, _ := refs.NewEpisode(1, 1)
	writeTrashEntry(t, root, bookwormRef, episode,
		time.Date(2026, 4, 20, 3, 0, 0, 0, time.UTC), "bw.mkv", "x")
	writeTrashEntry(t, root, otonariRef, episode,
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), "ot.mkv", "yy")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"trash", "list", "--json", "--all"}, testRunContextWithLibraryRoot(&stdout, &stderr, root)); err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	if total := int(result["totalEntries"].(float64)); total != 2 {
		t.Fatalf("totalEntries = %d, want 2", total)
	}
	if total := int(result["totalBytes"].(float64)); total != 3 {
		t.Fatalf("totalBytes = %d, want 3", total)
	}
}

func TestTrashListRequiresSelectorOrAll(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := run([]string{"trash", "list"}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err == nil || !strings.Contains(err.Error(), "selector terms or --all") {
		t.Fatalf("err = %v, want selector-or-all message", err)
	}
}

func TestTrashListRejectsBothSelectorAndAll(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := run([]string{"trash", "list", "--all", "Bookworm"}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err == nil || !strings.Contains(err.Error(), "either selector terms or --all") {
		t.Fatalf("err = %v, want selector-xor-all message", err)
	}
}

func TestTrashListOlderThanFilters(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()
	root := t.TempDir()
	ref := setupTrashSeries(t, root, "Bookworm", "370070")
	episode, _ := refs.NewEpisode(1, 1)
	old := writeTrashEntry(t, root, ref, episode,
		time.Now().UTC().Add(-48*time.Hour), "old.mkv", "x")
	_ = writeTrashEntry(t, root, ref, episode,
		time.Now().UTC().Add(-1*time.Hour), "fresh.mkv", "yy")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"trash", "list", "--json", "--tvdb-base-url", server.URL, "--older-than", "24h", "tvdb:370070"},
		testRunContextWithLibraryRoot(&stdout, &stderr, root)); err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	series := result["series"].([]any)
	if len(series) != 1 {
		t.Fatalf("len(series) = %d, want 1", len(series))
	}
	entries := series[0].(map[string]any)["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1 (only old entry past 24h threshold)", len(entries))
	}
	if entries[0].(map[string]any)["id"] != old.String() {
		t.Fatalf("id = %v, want %s", entries[0].(map[string]any)["id"], old.String())
	}
}

func TestTrashEmptyAllRequiresConfirm(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := run([]string{"trash", "empty", "--all"}, testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("err = %v, want --confirm message", err)
	}
}

func TestTrashEmptyAllWithConfirmRemovesEverything(t *testing.T) {
	root := t.TempDir()
	bookwormRef := setupTrashSeries(t, root, "Bookworm", "370070")
	otonariRef := setupTrashSeries(t, root, "Otonari", "999999")
	episode, _ := refs.NewEpisode(1, 1)
	writeTrashEntry(t, root, bookwormRef, episode,
		time.Date(2026, 4, 20, 3, 0, 0, 0, time.UTC), "bw.mkv", "x")
	writeTrashEntry(t, root, otonariRef, episode,
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), "ot.mkv", "yy")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"trash", "empty", "--json", "--all", "--confirm"},
		testRunContextWithLibraryRoot(&stdout, &stderr, root)); err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	if total := int(result["totalEntries"].(float64)); total != 2 {
		t.Fatalf("totalEntries = %d, want 2", total)
	}
	for _, dir := range []string{
		paths.TrashDir(root, bookwormRef),
		paths.TrashDir(root, otonariRef),
	} {
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Fatalf("trash dir %q non-empty after empty: %d entries", dir, len(entries))
		}
	}
}

func TestTrashRestoreMovesFilesBack(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()
	root := t.TempDir()
	ref := setupTrashSeries(t, root, "Bookworm", "370070")
	episode, _ := refs.NewEpisode(1, 3)
	id := writeTrashEntry(t, root, ref, episode,
		time.Date(2026, 4, 20, 3, 0, 0, 0, time.UTC),
		"old episode.mkv", "episode-body")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"trash", "restore", "--json", "--tvdb-base-url", server.URL, "tvdb:370070", id.String()},
		testRunContextWithLibraryRoot(&stdout, &stderr, root)); err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	restoredPath := filepath.Join(root, "Bookworm", "Season 1", "old episode.mkv")
	if _, err := os.Stat(restoredPath); err != nil {
		t.Fatalf("restored file missing at %s: %v", restoredPath, err)
	}
	if _, err := os.Stat(paths.TrashEntry(root, ref, id.String())); !os.IsNotExist(err) {
		t.Fatalf("trash entry still present after restore: err=%v", err)
	}
}

func TestTrashRestoreRefusesIfTargetExists(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()
	root := t.TempDir()
	ref := setupTrashSeries(t, root, "Bookworm", "370070")
	seasonDir := filepath.Join(root, "Bookworm", "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(seasonDir, "old episode.mkv"), "different content")
	episode, _ := refs.NewEpisode(1, 3)
	id := writeTrashEntry(t, root, ref, episode,
		time.Date(2026, 4, 20, 3, 0, 0, 0, time.UTC),
		"old episode.mkv", "episode-body")

	var stdout, stderr bytes.Buffer
	err := run([]string{"trash", "restore", "--tvdb-base-url", server.URL, "tvdb:370070", id.String()},
		testRunContextWithLibraryRoot(&stdout, &stderr, root))
	var existsErr *workflow.TrashRestoreTargetExistsError
	if !errors.As(err, &existsErr) {
		t.Fatalf("err = %v, want TrashRestoreTargetExistsError", err)
	}
	if _, err := os.Stat(paths.TrashEntry(root, ref, id.String())); err != nil {
		t.Fatalf("trash entry removed despite refusal: %v", err)
	}
}

func TestRemoveDefaultUntracksSeries(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()
	root := t.TempDir()
	ref := setupTrashSeries(t, root, "Bookworm", "370070")
	seriesDir := filepath.Join(root, ref.String())
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv")
	writeFile(t, mediaPath, "episode")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"remove", "--json", "--tvdb-base-url", server.URL, "tvdb:370070"},
		testRunContextWithLibraryRoot(&stdout, &stderr, root)); err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	if _, ok := result["reclaimedBytes"]; !ok {
		t.Fatalf("reclaimedBytes missing from result: %v", result)
	}
	if _, err := os.Stat(filepath.Join(seriesDir, ".kura")); !os.IsNotExist(err) {
		t.Fatalf(".kura still present after untrack: err=%v", err)
	}
	if _, err := os.Stat(mediaPath); err != nil {
		t.Fatalf("media file missing after untrack: %v", err)
	}
	if _, err := os.Stat(seriesDir); err != nil {
		t.Fatalf("series dir missing after untrack: %v", err)
	}
}

func TestRemovePurgeRequiresConfirm(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := run([]string{"remove", "--purge", "tvdb:370070"},
		testRunContextWithLibraryRoot(&stdout, &stderr, root))
	if err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("err = %v, want --confirm message", err)
	}
}

func TestRemovePurgeDeletesEverything(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()
	root := t.TempDir()
	ref := setupTrashSeries(t, root, "Bookworm", "370070")
	seriesDir := filepath.Join(root, ref.String())
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(seasonDir, "episode.mkv"), "episode")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"remove", "--purge", "--confirm", "--json", "--tvdb-base-url", server.URL, "tvdb:370070"},
		testRunContextWithLibraryRoot(&stdout, &stderr, root)); err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	if _, ok := result["reclaimedBytes"]; !ok {
		t.Fatalf("reclaimedBytes missing from result: %v", result)
	}
	if _, err := os.Stat(seriesDir); !os.IsNotExist(err) {
		t.Fatalf("series dir still present after purge: err=%v", err)
	}
}

func TestRemoveDefaultRefusesIfStaged(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S01E0001": {
				"airDate": "2019-10-03",
				"staged": {
					"path": "/inbox/Bookworm S01E01.mkv",
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

	var stdout, stderr bytes.Buffer
	err := run([]string{"remove", "--tvdb-base-url", server.URL, "tvdb:370070"},
		testRunContextWithLibraryRoot(&stdout, &stderr, root))
	var stagedErr *workflow.RemoveStagedRecordsExistError
	if !errors.As(err, &stagedErr) {
		t.Fatalf("err = %v, want RemoveStagedRecordsExistError", err)
	}
	if _, err := os.Stat(filepath.Join(seriesDir, ".kura", "series.json")); err != nil {
		t.Fatalf(".kura removed despite staged-records gate: %v", err)
	}
}

func TestRemovePurgeBypassesStagedGate(t *testing.T) {
	server := newCLITestServer(t)
	defer server.Close()
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S01E0001": {
				"airDate": "2019-10-03",
				"staged": {
					"path": "/inbox/Bookworm S01E01.mkv",
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

	var stdout, stderr bytes.Buffer
	if err := run([]string{"remove", "--purge", "--confirm", "--tvdb-base-url", server.URL, "tvdb:370070"},
		testRunContextWithLibraryRoot(&stdout, &stderr, root)); err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}
	if _, err := os.Stat(seriesDir); !os.IsNotExist(err) {
		t.Fatalf("series dir still present after purge: err=%v", err)
	}
}

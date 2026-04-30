package kura

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestScanCommits(t *testing.T) {
	server := newTestTVDBServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S01E0001": {"season": 1, "episode": 1, "airDate": "2019-10-03"}
		}
	}`)
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv"), "episode")

	lib := newTestLibraryWithMediaInfo(t, root, server.URL, newFakeMediaInfoCommand(t, root))
	series, err := lib.Get("Bookworm")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	result, err := series.Scan(context.Background(), ScanInput{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Synced) != 1 || result.Synced[0].Status != ScanStatusNew {
		t.Fatalf("Synced = %#v, want one new entry", result.Synced)
	}
	loaded, err := lib.Get("Bookworm")
	if err != nil {
		t.Fatalf("Get after scan: %v", err)
	}
	if len(loaded.Episodes()) != 1 {
		t.Fatalf("len(Episodes) = %d, want 1", len(loaded.Episodes()))
	}
}

func TestScanActiveCollisionReturnsTypedError(t *testing.T) {
	server := newTestTVDBServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S01E0001": {
				"season": 1,
				"episode": 1,
				"airDate": "2019-10-03",
				"active": {
					"path": "Season 1/existing.mkv",
					"source": "webrip",
					"size": 8,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			}
		}
	}`)
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv"), "episode")

	lib := newTestLibraryWithMediaInfo(t, root, server.URL, newFakeMediaInfoCommand(t, root))
	series, err := lib.Get("Bookworm")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_, err = series.Scan(context.Background(), ScanInput{})
	var tracked EpisodeAlreadyTrackedError
	if !errors.As(err, &tracked) {
		t.Fatalf("Scan error = %v, want EpisodeAlreadyTrackedError", err)
	}
}

func TestStageCommits(t *testing.T) {
	server := newTestTVDBServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S01E0001": {"season": 1, "episode": 1, "airDate": "2019-10-03"}
		}
	}`)
	mediaPath := filepath.Join(t.TempDir(), "Bookworm - S01E01 (WebRip).mkv")
	writeFile(t, mediaPath, "episode")

	lib := newTestLibraryWithMediaInfo(t, root, server.URL, newFakeMediaInfoCommand(t, root))
	series, err := lib.Get("Bookworm")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	result, err := series.Stage(context.Background(), StageInput{
		Season:    1,
		Episode:   1,
		MediaPath: mediaPath,
	})
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if !result.Applied || result.Entry.Media.Path != mediaPath {
		t.Fatalf("Stage result = %#v, want applied staged entry", result)
	}
	view, err := series.Read(context.Background(), ReadInput{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(view.Seasons) != 1 || len(view.Seasons[0].Episodes) != 1 || view.Seasons[0].Episodes[0].Staged == nil {
		t.Fatalf("read view = %#v, want staged episode in series metadata", view)
	}
}

func TestReconcilePlanApplyAndStalePlan(t *testing.T) {
	server := newTestTVDBServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "old.mkv"), "episode")
	writeFile(t, filepath.Join(seasonDir, "old.en.ass"), "subs")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S01E0001": {
				"season": 1,
				"episode": 1,
				"airDate": "2019-10-03",
				"active": {
					"path": "Season 1/old.mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"codec": "HEVC",
					"size": 7,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": [
						{"path": "Season 1/old.en.ass", "size": 4, "mtime": "2026-04-20T03:00:00Z"}
					]
				}
			}
		}
	}`)

	lib := newTestLibrary(t, root, server.URL)
	series, err := lib.Get("Bookworm")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	plan, err := series.PlanReconcile(context.Background(), ReconcileInput{})
	if err != nil {
		t.Fatalf("PlanReconcile: %v", err)
	}
	if !plan.HasChanges() || len(plan.Changes) != 1 || plan.Changes[0].Kind != ChangeMove {
		t.Fatalf("plan changes = %#v, want one move", plan.Changes)
	}
	if len(plan.Changes[0].Companions) != 1 {
		t.Fatalf("companion moves = %#v, want one companion move", plan.Changes[0].Companions)
	}

	stalePlan := plan
	seriesMetadataPath := filepath.Join(seriesDir, ".kura", "series.json")
	data, err := os.ReadFile(seriesMetadataPath)
	if err != nil {
		t.Fatalf("ReadFile series.json: %v", err)
	}
	if err := os.WriteFile(seriesMetadataPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile series.json: %v", err)
	}
	_, err = series.ApplyReconcile(context.Background(), stalePlan)
	var stale PlanStaleError
	if !errors.As(err, &stale) {
		t.Fatalf("Apply stale error = %v, want PlanStaleError", err)
	}

	plan, err = series.PlanReconcile(context.Background(), ReconcileInput{})
	if err != nil {
		t.Fatalf("PlanReconcile fresh: %v", err)
	}
	result, err := series.ApplyReconcile(context.Background(), plan)
	if err != nil {
		t.Fatalf("ApplyReconcile: %v", err)
	}
	if result.AppliedMoves != 2 {
		t.Fatalf("AppliedMoves = %d, want 2", result.AppliedMoves)
	}
	target := filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("Stat target: %v", err)
	}
	companionTarget := filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).en.ass")
	if _, err := os.Stat(companionTarget); err != nil {
		t.Fatalf("Stat companion target: %v", err)
	}
}

func newTestLibraryWithMediaInfo(t *testing.T, root string, tvdbBaseURL string, mediainfoCommand string) *Library {
	t.Helper()
	lib, err := New(Config{
		Root:               root,
		MediainfoCommand:   mediainfoCommand,
		TVDBKey:            "key",
		TVDBBaseURL:        tvdbBaseURL,
		PreferredLanguages: []string{"eng"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return lib
}

func newFakeMediaInfoCommand(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-mediainfo")
	script := `#!/bin/sh
cat <<'JSON'
{
  "media": {
    "track": [
      {"@type": "Video", "Format": "HEVC", "Width": "1920", "Height": "1080"}
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

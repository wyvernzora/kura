package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/media/mediainfo"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/progress"
	seriespkg "github.com/wyvernzora/kura/internal/series"
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
			"S01E0001": {"airDate": "2019-10-03"}
		}
	}`)
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv"), "episode")

	lib := newTestLibraryWithMediaInfo(t, root, server.URL, newFakeMediaInfoCommand(t, root))
	series, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	result, err := series.Scan(context.Background(), seriespkg.ScanInput{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Synced) != 1 || result.Synced[0].Status != seriespkg.ScanStatusAdded {
		t.Fatalf("Synced = %#v, want one added entry", result.Synced)
	}
	loaded, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Get after scan: %v", err)
	}
	view, err := loaded.Read(context.Background(), seriespkg.ReadInput{})
	if err != nil {
		t.Fatalf("Read after scan: %v", err)
	}
	if len(view.Seasons) != 1 || len(view.Seasons[0].Episodes) != 1 {
		t.Fatalf("view = %#v, want one episode", view)
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
	writeFile(t, filepath.Join(seasonDir, "existing.mkv"), "existing")
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv"), "episode")

	lib := newTestLibraryWithMediaInfo(t, root, server.URL, newFakeMediaInfoCommand(t, root))
	series, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_, err = series.Scan(context.Background(), seriespkg.ScanInput{})
	var tracked seriespkg.EpisodeAlreadyExistsError
	if !errors.As(err, &tracked) {
		t.Fatalf("Scan error = %v, want EpisodeAlreadyTrackedError", err)
	}
}

func TestScanWithStagedRecordsReturnsTypedError(t *testing.T) {
	server := newTestTVDBServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	stageDir := filepath.Join(root, "stage")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatalf("MkdirAll stage: %v", err)
	}
	stagedPath := filepath.Join(stageDir, "Bookworm - S01E01 (WebRip 1080p).mkv")
	writeFile(t, stagedPath, "staged episode")
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
					"size": 14,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			}
		}
	}`, stagedPath))
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv"), "episode")

	lib := newTestLibraryWithMediaInfo(t, root, server.URL, newFakeMediaInfoCommand(t, root))
	series, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err = series.Scan(context.Background(), seriespkg.ScanInput{})
	var staged seriespkg.ScanStagedRecordsError
	if !errors.As(err, &staged) {
		t.Fatalf("Scan error = %v, want ScanStagedRecordsError", err)
	}
	if len(staged.Episodes) != 1 || staged.Episodes[0].Marker() != "S01E01" {
		t.Fatalf("staged episodes = %#v, want S01E01", staged.Episodes)
	}
}

func TestScanWithStagedRecordsDoesNotFetchMetadata(t *testing.T) {
	root, err := ParseRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seriesDir := root.Join("Bookworm")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll series: %v", err)
	}
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S00E0001": {
				"airDate": "2019-10-03",
				"staged": {
					"path": "/inbox/special.mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"codec": "HEVC",
					"size": 14,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			}
		}
	}`)

	lib := New(root, failingSource{t: t}, mediainfo.Inspector{}, NewIndex(root))
	series, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err = series.Scan(context.Background(), seriespkg.ScanInput{})
	var staged seriespkg.ScanStagedRecordsError
	if !errors.As(err, &staged) {
		t.Fatalf("Scan error = %v, want ScanStagedRecordsError", err)
	}
	if len(staged.Episodes) != 1 || staged.Episodes[0].Marker() != "S00E01" {
		t.Fatalf("staged episodes = %#v, want S00E01", staged.Episodes)
	}
}

func TestScanReportsUnchangedUpdatedAndRemoved(t *testing.T) {
	server := newTestTVDBServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	unchangedPath := filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv")
	updatedPath := filepath.Join(seasonDir, "Bookworm - S01E02 (WebRip 1080p).mkv")
	writeFile(t, unchangedPath, "episode 1")
	writeFile(t, updatedPath, "episode 2 changed")
	unchangedInfo, err := os.Stat(unchangedPath)
	if err != nil {
		t.Fatalf("Stat unchanged: %v", err)
	}
	updatedInfo, err := os.Stat(updatedPath)
	if err != nil {
		t.Fatalf("Stat updated: %v", err)
	}
	writeSeriesJSON(t, seriesDir, fmt.Sprintf(`{
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
					"size": %d,
					"mtime": %q,
					"companions": []
				}
			},
			"S01E0002": {
				"airDate": "2019-10-10",
				"active": {
					"path": "Season 1/Bookworm - S01E02 (WebRip 1080p).mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"codec": "HEVC",
					"size": %d,
					"mtime": %q,
					"companions": []
				}
			},
			"S01E0003": {
				"airDate": "2019-10-17",
				"active": {
					"path": "Season 1/missing.mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"codec": "HEVC",
					"size": 7,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			}
		}
	}`, unchangedInfo.Size(), unchangedInfo.ModTime().UTC().Format(time.RFC3339), updatedInfo.Size()+1, updatedInfo.ModTime().UTC().Format(time.RFC3339)))

	lib := newTestLibraryWithMediaInfo(t, root, server.URL, newFakeMediaInfoCommand(t, root))
	series, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	result, err := series.Scan(context.Background(), seriespkg.ScanInput{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	statuses := map[refs.Episode]seriespkg.ScanStatus{}
	for _, synced := range result.Synced {
		statuses[synced.Episode] = synced.Status
	}
	for marker, want := range map[string]seriespkg.ScanStatus{
		"S01E0001": seriespkg.ScanStatusUnchanged,
		"S01E0002": seriespkg.ScanStatusUpdated,
		"S01E0003": seriespkg.ScanStatusRemoved,
	} {
		ref, err := refs.ParseEpisode(marker)
		if err != nil {
			t.Fatalf("ParseEpisode %s: %v", marker, err)
		}
		if got := statuses[ref]; got != want {
			t.Fatalf("%s status = %q, want %q; synced = %#v", marker, got, want, result.Synced)
		}
	}
	loaded, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Open after scan: %v", err)
	}
	view, err := loaded.Read(context.Background(), seriespkg.ReadInput{})
	if err != nil {
		t.Fatalf("Read after scan: %v", err)
	}
	removedRef, _ := refs.ParseEpisode("S01E0003")
	for _, episode := range view.Seasons[0].Episodes {
		if episode.Episode == removedRef && episode.Active != nil {
			t.Fatalf("removed episode active = %#v, want nil", episode.Active)
		}
	}
}

func TestScanReportsProgressForEachInspectedFile(t *testing.T) {
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
			"S01E0001": {"airDate": "2019-10-03"},
			"S01E0002": {"airDate": "2019-10-10"}
		}
	}`)
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv"), "episode 1")
	writeFile(t, filepath.Join(seasonDir, "Bookworm - S01E02 (WebRip 1080p).mkv"), "episode 2")

	lib := newTestLibraryWithMediaInfo(t, root, server.URL, newFakeMediaInfoCommand(t, root))
	series, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var events []progress.Event
	ctx := progress.With(context.Background(), func(_ context.Context, event progress.Event) {
		if event.Stage == "scan" && event.Status == progress.UpdateStatus {
			events = append(events, event)
		}
	})
	if _, err := series.Scan(ctx, seriespkg.ScanInput{}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var inspecting []progress.Event
	for _, event := range events {
		if strings.HasPrefix(event.Message, "Inspecting Bookworm - S01E") {
			inspecting = append(inspecting, event)
		}
	}
	if len(inspecting) != 2 {
		t.Fatalf("inspecting events = %#v, want two per-file events", inspecting)
	}
	for index, event := range inspecting {
		if event.Current != index+1 || event.Total != 2 {
			t.Fatalf("event %d = %#v, want current %d total 2", index, event, index+1)
		}
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
			"S01E0001": {"airDate": "2019-10-03"}
		}
	}`)
	mediaPath := filepath.Join(t.TempDir(), "Bookworm - S01E01 (WebRip).mkv")
	writeFile(t, mediaPath, "episode")

	lib := newTestLibraryWithMediaInfo(t, root, server.URL, newFakeMediaInfoCommand(t, root))
	series, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	episode, err := refs.NewEpisode(1, 1)
	if err != nil {
		t.Fatalf("NewEpisode: %v", err)
	}
	result, err := series.Stage(context.Background(), seriespkg.StageInput{
		Episode:   episode,
		MediaPath: mediaPath,
	})
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if !result.Applied || result.Record.Path != mediaPath {
		t.Fatalf("Stage result = %#v, want applied staged entry", result)
	}
	view, err := series.Read(context.Background(), seriespkg.ReadInput{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(view.Seasons) != 1 || len(view.Seasons[0].Episodes) != 1 || view.Seasons[0].Episodes[0].Staged == nil {
		t.Fatalf("read view = %#v, want staged episode in series metadata", view)
	}
}

func TestResetClearsStagedEpisode(t *testing.T) {
	server := newTestTVDBServer(t)
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

	lib := newTestLibraryWithMediaInfo(t, root, server.URL, newFakeMediaInfoCommand(t, root))
	series, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	episode, err := refs.NewEpisode(1, 1)
	if err != nil {
		t.Fatalf("NewEpisode: %v", err)
	}
	result, err := series.Reset(context.Background(), seriespkg.ResetInput{Episode: episode})
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if !result.Applied || result.Record == nil || result.Record.Path != stagedPath {
		t.Fatalf("Reset result = %#v, want applied staged record", result)
	}
	view, err := series.Read(context.Background(), seriespkg.ReadInput{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(view.Seasons) != 1 || len(view.Seasons[0].Episodes) != 1 || view.Seasons[0].Episodes[0].Staged != nil {
		t.Fatalf("read view = %#v, want no staged episode", view)
	}
}

func TestResetAllClearsEveryStagedEpisode(t *testing.T) {
	server := newTestTVDBServer(t)
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

	lib := newTestLibraryWithMediaInfo(t, root, server.URL, newFakeMediaInfoCommand(t, root))
	series, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	result, err := series.Reset(context.Background(), seriespkg.ResetInput{All: true})
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if !result.Applied || len(result.Records) != 2 {
		t.Fatalf("Reset result = %#v, want two cleared staged records", result)
	}
	if result.Records[0].Episode.String() != "S01E0001" || result.Records[0].Record.Path != firstPath {
		t.Fatalf("first cleared record = %#v, want S01E0001", result.Records[0])
	}
	if result.Records[1].Episode.String() != "S01E0002" || result.Records[1].Record.Path != secondPath {
		t.Fatalf("second cleared record = %#v, want S01E0002", result.Records[1])
	}
	view, err := series.Read(context.Background(), seriespkg.ReadInput{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for _, season := range view.Seasons {
		for _, episode := range season.Episodes {
			if episode.Staged != nil {
				t.Fatalf("read view = %#v, want no staged episodes", view)
			}
		}
	}
}

func TestResetAllNoStagedRecordsIsNoop(t *testing.T) {
	server := newTestTVDBServer(t)
	defer server.Close()

	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S01E0001": {"airDate": "2019-10-03"}
		}
	}`)

	lib := newTestLibraryWithMediaInfo(t, root, server.URL, newFakeMediaInfoCommand(t, root))
	series, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	result, err := series.Reset(context.Background(), seriespkg.ResetInput{All: true})
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if result.Applied || len(result.Records) != 0 {
		t.Fatalf("Reset result = %#v, want no-op success", result)
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
	series, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	plan, err := series.PlanReconcile()
	if err != nil {
		t.Fatalf("PlanReconcile: %v", err)
	}
	if !plan.HasChanges() || len(plan.Changes) != 1 || plan.Changes[0].Kind != seriespkg.ChangeMove {
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
	var stale seriespkg.PlanStaleError
	if !errors.As(err, &stale) {
		t.Fatalf("Apply stale error = %v, want PlanStaleError", err)
	}

	plan, err = series.PlanReconcile()
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
	lib, err := Open(Config{
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

type failingSource struct {
	fakeSource
	t *testing.T
}

func (s failingSource) GetSeries(context.Context, string) (metadata.Series, error) {
	s.t.Fatal("GetSeries called; staged scan should fail before provider fetch")
	return metadata.Series{}, nil
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

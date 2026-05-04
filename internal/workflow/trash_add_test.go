package workflow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/internal/workflow"
)

// writeMedia drops a file at <seriesRoot>/<rel>, creating parents.
func writeMedia(t *testing.T, seriesRoot, rel, body string) string {
	t.Helper()
	abs := filepath.Join(seriesRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return abs
}

// TestTrashAdd_MovesUntrackedFileToTrash is the happy path: a file
// sits in the series dir, isn't recorded as active or staged, and
// TrashAdd moves it (plus its filename-matched companion) into the
// trash directory.
func TestTrashAdd_MovesUntrackedFileToTrash(t *testing.T) {
	deps, ref := seedSeries(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)

	mediaAbs := writeMedia(t, seriesRoot, "Season 1/Show A - S01E01 (BluRay 1080p).mkv", "media-bytes")
	companionAbs := writeMedia(t, seriesRoot, "Season 1/Show A - S01E01 (BluRay 1080p).en.ass", "subs")

	out, err := workflow.TrashAdd(context.Background(), deps, workflow.TrashAddInput{
		Ref:  ref,
		Path: "Season 1/Show A - S01E01 (BluRay 1080p).mkv",
	})
	if err != nil {
		t.Fatalf("TrashAdd: %v", err)
	}
	if out.ID == "" {
		t.Fatal("ID empty")
	}
	wantEpisode, _ := refs.NewEpisode(1, 1)
	if out.Episode != wantEpisode {
		t.Fatalf("Episode = %s, want %s", out.Episode, wantEpisode)
	}
	if out.MediaPath != "Season 1/Show A - S01E01 (BluRay 1080p).mkv" {
		t.Fatalf("MediaPath = %q", out.MediaPath)
	}
	if len(out.Companions) != 1 || out.Companions[0] != "Season 1/Show A - S01E01 (BluRay 1080p).en.ass" {
		t.Fatalf("Companions = %v", out.Companions)
	}
	// Source files moved away.
	if _, err := os.Stat(mediaAbs); !os.IsNotExist(err) {
		t.Fatalf("media still at %s: err=%v", mediaAbs, err)
	}
	if _, err := os.Stat(companionAbs); !os.IsNotExist(err) {
		t.Fatalf("companion still at %s: err=%v", companionAbs, err)
	}
	// Trash entry populated.
	entryDir := paths.TrashEntry(deps.LibRoot, ref, out.ID)
	if _, err := os.Stat(filepath.Join(entryDir, "Show A - S01E01 (BluRay 1080p).mkv")); err != nil {
		t.Fatalf("media missing in trash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(entryDir, "Show A - S01E01 (BluRay 1080p).en.ass")); err != nil {
		t.Fatalf("companion missing in trash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(entryDir, "meta.json")); err != nil {
		t.Fatalf("meta.json missing: %v", err)
	}
}

// TestTrashAdd_RefusesActiveRecord guards against silently displacing
// a tracked active record. Trashing a file that's currently the
// active media for any episode must error with TrashAddTargetTrackedError;
// the file must still be on disk afterwards.
func TestTrashAdd_RefusesActiveRecord(t *testing.T) {
	deps, ref := seedSeries(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)
	mediaAbs := writeMedia(t, seriesRoot, "Season 1/Show A - S01E01 (BluRay 1080p).mkv", "media")

	// Add the file as an active record.
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	episodeRef, _ := refs.NewEpisode(1, 1)
	model.Episodes[episodeRef] = series.Episode{
		Active: &media.Record{
			Path:       mediaAbs,
			Source:     media.SourceBluRay,
			Resolution: mustResolution(t, 1920, 1080),
			Size:       int64(len("media")),
			MTime:      time.Now().UTC().Truncate(time.Second),
		},
	}
	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("seed_active")); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}

	_, err = workflow.TrashAdd(context.Background(), deps, workflow.TrashAddInput{
		Ref:  ref,
		Path: "Season 1/Show A - S01E01 (BluRay 1080p).mkv",
	})
	var tracked *workflow.TrashAddTargetTrackedError
	if !errors.As(err, &tracked) {
		t.Fatalf("err = %v, want TrashAddTargetTrackedError", err)
	}
	if tracked.RecordKind != "active" {
		t.Fatalf("RecordKind = %q, want active", tracked.RecordKind)
	}
	if tracked.Episode != episodeRef {
		t.Fatalf("Episode = %s, want %s", tracked.Episode, episodeRef)
	}
	if _, err := os.Stat(mediaAbs); err != nil {
		t.Fatalf("file should remain on disk; stat err: %v", err)
	}
}

// TestTrashAdd_RefusesStagedRecord is the staged-side counterpart of
// the active-record refusal. A file queued for reconcile must not be
// silently removed; caller has to kura_reset first.
func TestTrashAdd_RefusesStagedRecord(t *testing.T) {
	deps, ref := seedSeries(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)
	mediaAbs := writeMedia(t, seriesRoot, "Season 1/Show A - S01E01 (WebRip 720p).mkv", "media")

	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	episodeRef, _ := refs.NewEpisode(1, 1)
	model.Episodes[episodeRef] = series.Episode{
		Staged: &media.Record{
			Path:       mediaAbs,
			Source:     media.SourceWebRip,
			Resolution: mustResolution(t, 1280, 720),
			Size:       int64(len("media")),
			MTime:      time.Now().UTC().Truncate(time.Second),
		},
	}
	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("seed_staged")); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}

	_, err = workflow.TrashAdd(context.Background(), deps, workflow.TrashAddInput{
		Ref:  ref,
		Path: "Season 1/Show A - S01E01 (WebRip 720p).mkv",
	})
	var tracked *workflow.TrashAddTargetTrackedError
	if !errors.As(err, &tracked) {
		t.Fatalf("err = %v, want TrashAddTargetTrackedError", err)
	}
	if tracked.RecordKind != "staged" {
		t.Fatalf("RecordKind = %q, want staged", tracked.RecordKind)
	}
	if _, err := os.Stat(mediaAbs); err != nil {
		t.Fatalf("file should remain on disk; stat err: %v", err)
	}
}

// TestTrashAdd_RefusesUnparseableFilename: orphan files (filename
// can't be parsed to a season/episode) require manual cleanup. The
// workflow refuses cleanly so callers can surface the limitation.
func TestTrashAdd_RefusesUnparseableFilename(t *testing.T) {
	deps, ref := seedSeries(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)
	writeMedia(t, seriesRoot, "Season 1/random-name.mkv", "x")

	_, err := workflow.TrashAdd(context.Background(), deps, workflow.TrashAddInput{
		Ref:  ref,
		Path: "Season 1/random-name.mkv",
	})
	var unparseable *workflow.TrashAddTargetUnparseableError
	if !errors.As(err, &unparseable) {
		t.Fatalf("err = %v, want TrashAddTargetUnparseableError", err)
	}
}

func mustResolution(t *testing.T, w, h int) media.Resolution {
	t.Helper()
	r, err := media.NewResolution(w, h)
	if err != nil {
		t.Fatalf("NewResolution: %v", err)
	}
	return r
}

package workflow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library/internal/coord"
	"github.com/wyvernzora/kura/services/library/internal/domain/media"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/domain/selector"
	"github.com/wyvernzora/kura/services/library/internal/domain/series"
	"github.com/wyvernzora/kura/services/library/internal/jobs"
	"github.com/wyvernzora/kura/services/library/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

// fakeInspector satisfies media.Inspector with canned per-path results.
// Empty info returned when the path is unknown — callers should explicitly
// register the paths they want recognized.
type fakeInspector struct {
	infos map[string]media.Info
	err   error
}

func (f *fakeInspector) Inspect(_ context.Context, path string) (media.Info, error) {
	if f.err != nil {
		return media.Info{}, f.err
	}
	if info, ok := f.infos[path]; ok {
		return info, nil
	}
	return media.Info{}, nil
}

// seedStageDeps returns deps prewired with a Jobs registry, a fake
// inspector, a deterministic Now, and an inbox tempdir wired into
// deps.InboxRoot. The returned inboxDir is where episode/extras source
// files should be written so stage's selector resolution finds them.
func seedStageDeps(t *testing.T) (deps workflow.Deps, ref refs.Series, insp *fakeInspector, inboxDir string) {
	t.Helper()
	deps, ref = seedSeries(t)
	deps.Jobs = jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { deps.Jobs.Shutdown(0) })
	insp = &fakeInspector{infos: map[string]media.Info{}}
	deps.Inspector = insp
	deps.Now = func() time.Time { return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC) }
	inboxDir = t.TempDir()
	deps.InboxRoot = inboxDir

	// Seed an episode in the spine so tests can stage to it.
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e1, _ := refs.NewEpisode(1, 1)
	e2, _ := refs.NewEpisode(1, 2)
	model.Episodes[e1] = series.Episode{}
	model.Episodes[e2] = series.Episode{}
	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("seed_spine")); err != nil {
		t.Fatalf("SaveCAS spine: %v", err)
	}
	return deps, ref, insp, inboxDir
}

// writeInboxMedia writes a file at <inboxRoot>/<rel> with the given
// body and returns an inbox: selector pointing at it. Mirror of
// writeMedia for inbox-rooted source files.
func writeInboxMedia(t *testing.T, inboxRoot, rel, body string) selector.Path {
	t.Helper()
	abs := filepath.Join(inboxRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sel, err := selector.ParseInbox("inbox:" + rel)
	if err != nil {
		t.Fatalf("ParseSelector: %v", err)
	}
	return sel
}

// writeInboxDir creates a directory at <inboxRoot>/<rel> and returns
// an inbox: selector for it. Used by extras tests where the source is
// a folder.
func writeInboxDir(t *testing.T, inboxRoot, rel string) selector.Path {
	t.Helper()
	abs := filepath.Join(inboxRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sel, err := selector.ParseInbox("inbox:" + rel)
	if err != nil {
		t.Fatalf("ParseSelector: %v", err)
	}
	return sel
}

func TestStage_AlwaysReturnsTrackedJob(t *testing.T) {
	deps, ref, _, _ := seedStageDeps(t)
	j := workflow.Stage(context.Background(), deps, workflow.StageInput{Ref: ref})
	if !j.IsTracked() {
		t.Fatalf("Stage must return a tracked job; IsTracked=false")
	}
	if j.Kind() != string(jobs.KindStage) {
		t.Fatalf("Job.Kind = %q, want %q", j.Kind(), jobs.KindStage)
	}
}

func TestStage_RejectsEmptyBatch(t *testing.T) {
	deps, ref, _, _ := seedStageDeps(t)
	j := workflow.Stage(context.Background(), deps, workflow.StageInput{Ref: ref})
	_, err := j.Wait(context.Background())
	if _, ok := errors.AsType[*workflow.EmptyStageBatchError](err); !ok {
		t.Fatalf("err = %v, want EmptyStageBatchError", err)
	}
}

func TestStage_EpisodeAttrsPersistAndSurface(t *testing.T) {
	deps, ref, insp, inboxDir := seedStageDeps(t)
	sel := writeInboxMedia(t, inboxDir, "ep1.mkv", "body")
	insp.infos[sel.Resolve(inboxDir)] = media.Info{Resolution: "1920x1080"}
	e1, _ := refs.NewEpisode(1, 1)

	j := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref: ref,
		Episodes: []workflow.EpisodeStageItem{{
			Episode: e1,
			Media:   sel,
			Attrs: media.Attrs{
				"origin":        "takuhai",
				"release_group": "SubsPlease",
			},
		}},
	})
	out, err := j.Wait(context.Background())
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if got := out.Episodes[0].Record.Attrs["release_group"]; got != "SubsPlease" {
		t.Fatalf("result attrs release_group = %q", got)
	}
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatal(err)
	}
	if got := model.Episodes[e1].Staged.Attrs["origin"]; got != "takuhai" {
		t.Fatalf("persisted origin = %q", got)
	}
}

func TestStage_RejectsInvalidAttrs(t *testing.T) {
	deps, ref, insp, inboxDir := seedStageDeps(t)
	sel := writeInboxMedia(t, inboxDir, "ep1.mkv", "body")
	insp.infos[sel.Resolve(inboxDir)] = media.Info{Resolution: "1920x1080"}
	e1, _ := refs.NewEpisode(1, 1)

	j := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref: ref,
		Episodes: []workflow.EpisodeStageItem{{
			Episode: e1,
			Media:   sel,
			Attrs:   media.Attrs{"BadKey": "value"},
		}},
	})
	_, err := j.Wait(context.Background())
	if err == nil || !strings.Contains(err.Error(), "attrs") {
		t.Fatalf("Stage err = %v, want attrs validation error", err)
	}
}

func TestStage_TrashRefusesActiveRecord(t *testing.T) {
	deps, ref, _, _ := seedStageDeps(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)
	rel := "Season 1/active.mkv"
	mediaPath := writeMedia(t, seriesRoot, rel, "body")

	// Mark active record on the spine slot.
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e1, _ := refs.NewEpisode(1, 1)
	if err := model.SetActive(e1, media.Record{Path: mediaPath, Companions: []media.Companion{}}); err != nil {
		t.Fatal(err)
	}
	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("seed_active")); err != nil {
		t.Fatal(err)
	}

	pathSel := selector.Path{Scheme: selector.Series, Relative: rel}
	j := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref:   ref,
		Trash: []workflow.TrashStageItem{{Path: pathSel}},
	})
	_, err = j.Wait(context.Background())
	if _, ok := errors.AsType[*workflow.TrashTargetTrackedError](err); !ok {
		t.Fatalf("err = %v, want TrashTargetTrackedError", err)
	}
}

func TestStage_DuplicateEpisodeInBatch(t *testing.T) {
	deps, ref, insp, inboxDir := seedStageDeps(t)
	aSel := writeInboxMedia(t, inboxDir, "a.mkv", "a")
	bSel := writeInboxMedia(t, inboxDir, "b.mkv", "b")
	insp.infos[aSel.Resolve(inboxDir)] = media.Info{}
	insp.infos[bSel.Resolve(inboxDir)] = media.Info{}
	e1, _ := refs.NewEpisode(1, 1)
	j := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref: ref,
		Episodes: []workflow.EpisodeStageItem{
			{Episode: e1, Media: aSel},
			{Episode: e1, Media: bSel},
		},
	})
	_, err := j.Wait(context.Background())
	if _, ok := errors.AsType[*workflow.DuplicateBatchEpisodeError](err); !ok {
		t.Fatalf("err = %v, want DuplicateBatchEpisodeError", err)
	}
}

func TestStage_ExtraSeasonMissing(t *testing.T) {
	deps, ref, _, inboxDir := seedStageDeps(t)
	src := writeInboxDir(t, inboxDir, "bts")
	j := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref:    ref,
		Extras: []workflow.ExtraStageItem{{Season: 99, Source: src}},
	})
	_, err := j.Wait(context.Background())
	if _, ok := errors.AsType[*workflow.ExtraSeasonMissingError](err); !ok {
		t.Fatalf("err = %v, want ExtraSeasonMissingError", err)
	}
}

func TestStage_ExtraPrefixInvalid(t *testing.T) {
	deps, ref, _, inboxDir := seedStageDeps(t)
	src := writeInboxMedia(t, inboxDir, "bts.mp4", "x")
	for _, bad := range []string{"sub/dir", "..", ".hidden", "."} {
		j := workflow.Stage(context.Background(), deps, workflow.StageInput{
			Ref:    ref,
			Extras: []workflow.ExtraStageItem{{Season: 1, Source: src, Prefix: bad}},
		})
		_, err := j.Wait(context.Background())
		if _, ok := errors.AsType[*workflow.ExtraPrefixInvalidError](err); !ok {
			t.Errorf("prefix %q: err = %v, want ExtraPrefixInvalidError", bad, err)
		}
	}
}

func TestStage_ExtraTargetCollisionOnDisk(t *testing.T) {
	deps, ref, _, inboxDir := seedStageDeps(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)
	// Plant something at the destination already.
	if err := os.MkdirAll(filepath.Join(seriesRoot, "Season 1", "Extra"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seriesRoot, "Season 1", "Extra", "bts.mp4"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := writeInboxMedia(t, inboxDir, "bts.mp4", "new")
	j := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref:    ref,
		Extras: []workflow.ExtraStageItem{{Season: 1, Source: src}},
	})
	_, err := j.Wait(context.Background())
	if _, ok := errors.AsType[*workflow.ExtraTargetCollisionError](err); !ok {
		t.Fatalf("err = %v, want ExtraTargetCollisionError", err)
	}
}

func TestStage_MultiItemHappyPath(t *testing.T) {
	deps, ref, insp, inboxDir := seedStageDeps(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)

	// Episode media + extras dir come from inbox; trash file lives in
	// the series root (trash is library-internal by design).
	winnerSel := writeInboxMedia(t, inboxDir, "winner.mkv", "winner")
	loserRel := "Season 1/loser.mkv"
	writeMedia(t, seriesRoot, loserRel, "loser")
	loserSel := selector.Path{Scheme: selector.Series, Relative: loserRel}
	insp.infos[winnerSel.Resolve(inboxDir)] = media.Info{Resolution: "1920x1080"}

	btsSel := writeInboxDir(t, inboxDir, "bts")

	e1, _ := refs.NewEpisode(1, 1)
	j := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref: ref,
		Episodes: []workflow.EpisodeStageItem{{
			Episode: e1,
			Media:   winnerSel,
		}},
		Trash:  []workflow.TrashStageItem{{Path: loserSel}},
		Extras: []workflow.ExtraStageItem{{Season: 1, Source: btsSel, Prefix: "bts-extras"}},
	})
	out, err := j.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if len(out.Episodes) != 1 || out.Episodes[0].Status != "staged" {
		t.Fatalf("Episodes = %+v", out.Episodes)
	}
	if len(out.Trash) != 1 {
		t.Fatalf("Trash = %+v", out.Trash)
	}
	if len(out.Extras) != 1 || out.Extras[0].Prefix != "bts-extras" {
		t.Fatalf("Extras = %+v", out.Extras)
	}
	if len(out.Skipped) != 0 {
		t.Fatalf("Skipped = %+v", out.Skipped)
	}

	// Verify series.json reflects all three.
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatal(err)
	}
	if model.Episodes[e1].Staged == nil {
		t.Fatal("episode 1.1 has no staged record after Stage")
	}
	if len(model.StagedTrash) != 1 {
		t.Fatalf("StagedTrash len = %d, want 1", len(model.StagedTrash))
	}
	if len(model.StagedExtras) != 1 {
		t.Fatalf("StagedExtras len = %d, want 1", len(model.StagedExtras))
	}
}

// TestStage_SeriesMediaInPlaceOverride covers the in-place metadata
// override path: stage a series: selector pointing at the existing
// active record's path, with replace=true and an explicit source. The
// staged record should land with the override source applied; the
// active record is unchanged until reconcile.
func TestStage_SeriesMediaInPlaceOverride(t *testing.T) {
	deps, ref, insp, _ := seedStageDeps(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)
	rel := "Season 1/episode-1.mkv"
	mediaPath := writeMedia(t, seriesRoot, rel, "body")

	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e1, _ := refs.NewEpisode(1, 1)
	if err := model.SetActive(e1, media.Record{Path: mediaPath, Companions: []media.Companion{}}); err != nil {
		t.Fatal(err)
	}
	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("seed_active")); err != nil {
		t.Fatal(err)
	}
	insp.infos[mediaPath] = media.Info{}

	mediaSel := selector.Path{Scheme: selector.Series, Relative: rel}
	j := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref: ref,
		Episodes: []workflow.EpisodeStageItem{{
			Episode: e1,
			Media:   mediaSel,
			Source:  "BluRay",
			Replace: true,
		}},
	})
	if _, err := j.Wait(context.Background()); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	model, err = seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load after stage: %v", err)
	}
	staged := model.Episodes[e1].Staged
	if staged == nil {
		t.Fatal("episode 1.1 has no staged record after in-place override")
	}
	// media.Source.String() lower-cases for storage; Display() returns
	// the cased label for human / wire output.
	if got := staged.Source.Display(); got != "BluRay" {
		t.Fatalf("staged.Source.Display() = %q, want BluRay", got)
	}
	if staged.Path != mediaSel.String() {
		t.Fatalf("staged.Path = %q, want %q", staged.Path, mediaSel.String())
	}
}

// TestStage_SeriesMediaRequiresReplace rejects series: media when
// replace is false (no contract for "stage but don't promote").
func TestStage_SeriesMediaRequiresReplace(t *testing.T) {
	deps, ref, _, _ := seedStageDeps(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)
	rel := "Season 1/episode-1.mkv"
	mediaPath := writeMedia(t, seriesRoot, rel, "body")

	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e1, _ := refs.NewEpisode(1, 1)
	if err := model.SetActive(e1, media.Record{Path: mediaPath, Companions: []media.Companion{}}); err != nil {
		t.Fatal(err)
	}
	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("seed_active")); err != nil {
		t.Fatal(err)
	}

	mediaSel := selector.Path{Scheme: selector.Series, Relative: rel}
	j := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref: ref,
		Episodes: []workflow.EpisodeStageItem{{
			Episode: e1,
			Media:   mediaSel,
			Replace: false,
		}},
	})
	if _, err := j.Wait(context.Background()); err == nil {
		t.Fatal("Stage with series: media + replace=false succeeded; want error")
	}
}

// TestStage_SeriesMediaCrossSlot stages a series-resident file into a
// different episode's empty slot. The file isn't claimed elsewhere, so
// the stage must succeed and persist the staged record under the
// target episode.
func TestStage_SeriesMediaCrossSlot(t *testing.T) {
	deps, ref, insp, _ := seedStageDeps(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)
	otherRel := "Season 1/loose.mkv"
	otherPath := writeMedia(t, seriesRoot, otherRel, "loose")
	insp.infos[otherPath] = media.Info{}

	e2, _ := refs.NewEpisode(1, 2)
	mediaSel := selector.Path{Scheme: selector.Series, Relative: otherRel}
	j := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref: ref,
		Episodes: []workflow.EpisodeStageItem{{
			Episode: e2,
			Media:   mediaSel,
		}},
	})
	if _, err := j.Wait(context.Background()); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	staged := model.Episodes[e2].Staged
	if staged == nil {
		t.Fatal("episode 1.2 has no staged record")
	}
	if staged.Path != mediaSel.String() {
		t.Fatalf("staged.Path = %q, want %q", staged.Path, mediaSel.String())
	}
}

// TestStage_SeriesMediaRejectsClaimedActivePath stages a series: file
// that's already the active record for ANOTHER episode. Must be
// rejected — the in-place exception only applies to the same episode's
// own active record.
func TestStage_SeriesMediaRejectsClaimedActivePath(t *testing.T) {
	deps, ref, _, _ := seedStageDeps(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)
	rel := "Season 1/episode-1.mkv"
	mediaPath := writeMedia(t, seriesRoot, rel, "body")

	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e1, _ := refs.NewEpisode(1, 1)
	e2, _ := refs.NewEpisode(1, 2)
	if err := model.SetActive(e1, media.Record{Path: mediaPath, Companions: []media.Companion{}}); err != nil {
		t.Fatal(err)
	}
	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("seed_active")); err != nil {
		t.Fatal(err)
	}

	mediaSel := selector.Path{Scheme: selector.Series, Relative: rel}
	j := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref: ref,
		Episodes: []workflow.EpisodeStageItem{{
			Episode: e2,
			Media:   mediaSel,
		}},
	})
	if _, err := j.Wait(context.Background()); err == nil {
		t.Fatal("Stage of E1's active path under E2 succeeded; want claimed-paths rejection")
	}
}

// TestStage_SeriesMediaRequiresReplaceWhenSlotOccupied: cross-slot
// stages still require replace=true if the target slot already has an
// active record (matches the inbox-stage rule).
func TestStage_SeriesMediaRequiresReplaceWhenSlotOccupied(t *testing.T) {
	deps, ref, insp, _ := seedStageDeps(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)
	activeRel := "Season 1/active.mkv"
	otherRel := "Season 1/loose.mkv"
	activePath := writeMedia(t, seriesRoot, activeRel, "active")
	otherPath := writeMedia(t, seriesRoot, otherRel, "loose")
	insp.infos[otherPath] = media.Info{}

	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e1, _ := refs.NewEpisode(1, 1)
	if err := model.SetActive(e1, media.Record{Path: activePath, Companions: []media.Companion{}}); err != nil {
		t.Fatal(err)
	}
	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("seed_active")); err != nil {
		t.Fatal(err)
	}

	mediaSel := selector.Path{Scheme: selector.Series, Relative: otherRel}
	j := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref: ref,
		Episodes: []workflow.EpisodeStageItem{{
			Episode: e1,
			Media:   mediaSel,
			// Replace omitted on purpose — slot has an active record.
		}},
	})
	if _, err := j.Wait(context.Background()); err == nil {
		t.Fatal("Stage over an active slot without replace=true succeeded; want EpisodeAlreadyExistsError")
	}
}

func TestStage_DuplicateTrashPathInBatch(t *testing.T) {
	deps, ref, _, _ := seedStageDeps(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)
	loserRel := "Season 1/loser.mkv"
	writeMedia(t, seriesRoot, loserRel, "x")
	loserSel := selector.Path{Scheme: selector.Series, Relative: loserRel}
	j := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref: ref,
		Trash: []workflow.TrashStageItem{
			{Path: loserSel},
			{Path: loserSel},
		},
	})
	_, err := j.Wait(context.Background())
	if _, ok := errors.AsType[*workflow.DuplicateBatchPathError](err); !ok {
		t.Fatalf("err = %v, want DuplicateBatchPathError", err)
	}
}

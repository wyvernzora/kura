package workflow_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/media"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/selector"
	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

// TestApplyReconcile_AlwaysReturnsTrackedJob is the per-workflow
// IsTracked invariant test. Long workflows MUST always go through
// jobs.Submit; the MCP long-tool handler relies on this to construct
// a JobHandle unconditionally per design/async-job.md § 11.10.
func TestApplyReconcile_AlwaysReturnsTrackedJob(t *testing.T) {
	deps, ref := seedSeries(t)
	deps.Jobs = jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { deps.Jobs.Shutdown(0) })

	j := workflow.ApplyReconcile(context.Background(), deps, workflow.ApplyReconcileInput{Ref: ref, Token: "deadbeefdead"})
	if !j.IsTracked() {
		t.Fatalf("ApplyReconcile must always return a tracked job; got IsTracked=false")
	}
	if j.ID() == "" {
		t.Fatalf("tracked job must have non-empty ID")
	}
	if j.Kind() != string(jobs.KindReconcileApply) {
		t.Fatalf("Job.Kind = %q, want %q", j.Kind(), jobs.KindReconcileApply)
	}
	// Don't Wait — closure will fail looking up the bogus token, but
	// that's a goroutine concern. Invariant is about Submit being
	// called regardless of input validity.
}

func TestApplyReconcile_MissingPlanReturnsCodedNotFound(t *testing.T) {
	deps, ref := seedSeries(t)
	deps.Jobs = jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { deps.Jobs.Shutdown(0) })

	j := workflow.ApplyReconcile(context.Background(), deps, workflow.ApplyReconcileInput{Ref: ref, Token: "deadbeefdead"})
	if _, err := j.Wait(context.Background()); err == nil {
		t.Fatal("Wait err = nil, want missing plan error")
	} else if coded, ok := err.(errkind.Coded); !ok {
		t.Fatalf("err = %T %[1]v, want coded error", err)
	} else if coded.Kind() != errkind.KindNotFound {
		t.Fatalf("kind = %s, want %s", coded.Kind(), errkind.KindNotFound)
	}
}

// End-to-end test: Stage episode + trash + extra, plan, apply. Verify
// all three moved correctly and series.json staging arrays cleared.
func TestReconcile_StagedTrashAndExtrasEndToEnd(t *testing.T) {
	deps, ref, insp, inboxDir := seedStageDeps(t)
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)

	winnerSel := writeInboxMedia(t, inboxDir, "winner.mkv", "winner")
	loserRel := "Season 1/loser.mkv"
	writeMedia(t, seriesRoot, loserRel, "loser")
	loserSel := selector.Path{Scheme: selector.Series, Relative: loserRel}
	insp.infos[winnerSel.Resolve(inboxDir)] = media.Info{Resolution: "1920x1080"}

	btsSel := writeInboxDir(t, inboxDir, "bts")
	bts := btsSel.Resolve(inboxDir)
	if err := os.WriteFile(filepath.Join(bts, "interview.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	e1, _ := refs.NewEpisode(1, 1)
	stageJob := workflow.Stage(context.Background(), deps, workflow.StageInput{
		Ref:      ref,
		Episodes: []workflow.EpisodeStageItem{{Episode: e1, Media: winnerSel}},
		Trash:    []workflow.TrashStageItem{{Path: loserSel}},
		Extras:   []workflow.ExtraStageItem{{Season: 1, Source: btsSel, Prefix: "behind-the-scenes"}},
	})
	if _, err := stageJob.Wait(context.Background()); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	// Plan.
	plan, err := workflow.PlanReconcile(context.Background(), deps, workflow.PlanReconcileInput{Ref: ref})
	if err != nil {
		t.Fatalf("PlanReconcile: %v", err)
	}
	if len(plan.Plan.TrashItems) != 1 {
		t.Fatalf("plan.TrashItems len = %d, want 1", len(plan.Plan.TrashItems))
	}
	if len(plan.Plan.Extras) != 1 {
		t.Fatalf("plan.Extras len = %d, want 1", len(plan.Plan.Extras))
	}
	if !plan.Plan.Extras[0].IsDir {
		t.Errorf("Extras[0].IsDir = false, want true (source is a directory)")
	}

	// Apply.
	applyJob := workflow.ApplyReconcile(context.Background(), deps, workflow.ApplyReconcileInput{Ref: ref, Token: plan.Token})
	if _, err := applyJob.Wait(context.Background()); err != nil {
		t.Fatalf("ApplyReconcile: %v", err)
	}

	// Verify: episode promoted, stagedTrash + stagedExtras drained.
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatal(err)
	}
	if model.Episodes[e1].Active == nil {
		t.Error("episode 1.1 has no active record after apply")
	}
	if model.Episodes[e1].Staged != nil {
		t.Error("episode 1.1 still has staged record after apply")
	}
	if len(model.StagedTrash) != 0 {
		t.Errorf("StagedTrash len = %d, want 0 after apply", len(model.StagedTrash))
	}
	if len(model.StagedExtras) != 0 {
		t.Errorf("StagedExtras len = %d, want 0 after apply", len(model.StagedExtras))
	}

	// Verify: files moved on disk.
	// Loser is gone from Season 1/.
	loserAbs := loserSel.Resolve(seriesRoot)
	if _, err := os.Stat(loserAbs); !os.IsNotExist(err) {
		t.Errorf("loser still present at %q (err=%v)", loserAbs, err)
	}
	// Trash bucket created with the source file.
	trashDir := paths.TrashDir(deps.LibRoot, ref)
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		t.Fatalf("ReadDir trash: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("trash entries = %d, want 1", len(entries))
	}
	// Extras placed at Season 1/Extra/behind-the-scenes/bts/.
	wantExtra := filepath.Join(seriesRoot, "Season 1", "Extra", "behind-the-scenes", "bts", "interview.mp4")
	if _, err := os.Stat(wantExtra); err != nil {
		t.Errorf("extras destination missing: %v", err)
	}
	// Source extras dir should be removed.
	if _, err := os.Stat(bts); !os.IsNotExist(err) {
		t.Errorf("extras source still present at %q", bts)
	}
}

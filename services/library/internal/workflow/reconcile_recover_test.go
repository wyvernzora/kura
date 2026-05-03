package workflow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/internal/workflow"
)

func newRecoverDeps(t *testing.T) (workflow.Deps, refs.Series, string) {
	t.Helper()
	root := t.TempDir()
	ref, err := refs.ParseSeries("Show A")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatalf("Hostname: %v", err)
	}
	model := &series.Series{
		Ref:      ref,
		Metadata: refs.Metadata("tvdb:42"),
		Episodes: map[refs.Episode]series.Episode{},
	}
	if err := seriesfile.SaveCAS(root, model, coord.Mutator{
		Op: "test_seed", PID: os.Getpid(), Host: host, At: time.Now().UTC().Truncate(time.Second),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	deps := workflow.Deps{
		LibRoot:     root,
		Coordinator: coord.NewCLICoordinator(),
		HostName:    host,
		Now:         time.Now,
	}
	return deps, ref, host
}

func plantClaim(t *testing.T, deps workflow.Deps, ref refs.Series, holder coord.Holder) {
	t.Helper()
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model.InProgress = &holder
	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.Mutator{
		Op: "test_plant", PID: os.Getpid(), Host: "h", At: time.Now().UTC().Truncate(time.Second),
	}); err != nil {
		t.Fatalf("plant: %v", err)
	}
}

func TestRecoverReconcile_NoClaimIsNoOp(t *testing.T) {
	deps, ref, _ := newRecoverDeps(t)
	out, err := workflow.RecoverReconcile(context.Background(), deps, workflow.RecoverReconcileInput{Ref: ref})
	if err != nil {
		t.Fatalf("RecoverReconcile: %v", err)
	}
	if out.Cleared {
		t.Fatal("Cleared = true with no claim, want false")
	}
}

func TestRecoverReconcile_RefusesAliveSameHost(t *testing.T) {
	deps, ref, host := newRecoverDeps(t)
	plantClaim(t, deps, ref, coord.Holder{
		Op:      "reconcile_apply",
		Token:   "abc123",
		PID:     os.Getpid(),
		Host:    host,
		Started: time.Now().UTC().Truncate(time.Second),
	})
	_, err := workflow.RecoverReconcile(context.Background(), deps, workflow.RecoverReconcileInput{Ref: ref})
	var busy *coord.BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("err = %v, want BusyError", err)
	}
}

func TestRecoverReconcile_BreaksStaleSameHost(t *testing.T) {
	deps, ref, host := newRecoverDeps(t)
	// PID 1 is alive on most systems, PID 0 is invalid (treated as dead).
	plantClaim(t, deps, ref, coord.Holder{
		Op:      "reconcile_apply",
		Token:   "abc123",
		PID:     0,
		Host:    host,
		Started: time.Now().UTC().Truncate(time.Second),
	})
	out, err := workflow.RecoverReconcile(context.Background(), deps, workflow.RecoverReconcileInput{Ref: ref})
	if err != nil {
		t.Fatalf("RecoverReconcile: %v", err)
	}
	if !out.Cleared {
		t.Fatal("Cleared = false, want true for stale claim")
	}
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if model.InProgress != nil {
		t.Fatalf("InProgress = %+v, want nil after recover", model.InProgress)
	}
}

func TestRecoverReconcile_RefusesCrossHostWithoutForce(t *testing.T) {
	deps, ref, _ := newRecoverDeps(t)
	plantClaim(t, deps, ref, coord.Holder{
		Op:      "reconcile_apply",
		Token:   "abc123",
		PID:     0, // would be stale by PID alone, but cross-host is opaque
		Host:    "elsewhere",
		Started: time.Now().UTC().Truncate(time.Second),
	})
	_, err := workflow.RecoverReconcile(context.Background(), deps, workflow.RecoverReconcileInput{Ref: ref})
	var busy *coord.BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("err = %v, want BusyError for cross-host without --force", err)
	}
}

func TestRecoverReconcile_BreaksCrossHostWithForce(t *testing.T) {
	deps, ref, _ := newRecoverDeps(t)
	plantClaim(t, deps, ref, coord.Holder{
		Op:      "reconcile_apply",
		Token:   "abc123",
		PID:     12345,
		Host:    "elsewhere",
		Started: time.Now().UTC().Truncate(time.Second),
	})
	out, err := workflow.RecoverReconcile(context.Background(), deps, workflow.RecoverReconcileInput{Ref: ref, Force: true})
	if err != nil {
		t.Fatalf("RecoverReconcile force: %v", err)
	}
	if !out.Cleared {
		t.Fatal("Cleared = false with --force, want true")
	}
	if out.PriorHolder == nil || out.PriorHolder.Host != "elsewhere" {
		t.Fatalf("PriorHolder = %+v", out.PriorHolder)
	}
}

func TestRecoverReconcile_ForceBreaksAliveSameHost(t *testing.T) {
	deps, ref, host := newRecoverDeps(t)
	plantClaim(t, deps, ref, coord.Holder{
		Op:      "reconcile_apply",
		Token:   "abc123",
		PID:     os.Getpid(),
		Host:    host,
		Started: time.Now().UTC().Truncate(time.Second),
	})
	out, err := workflow.RecoverReconcile(context.Background(), deps, workflow.RecoverReconcileInput{Ref: ref, Force: true})
	if err != nil {
		t.Fatalf("RecoverReconcile force: %v", err)
	}
	if !out.Cleared {
		t.Fatal("Cleared = false with --force, want true")
	}
}

// Sanity: claim file location matches what the helpers above wrote.
func TestRecoverReconcile_TargetsSeriesJSON(t *testing.T) {
	deps, ref, _ := newRecoverDeps(t)
	expected := filepath.Join(deps.LibRoot, ref.String(), ".kura", "series.json")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("series.json not at expected path %q: %v", expected, err)
	}
}

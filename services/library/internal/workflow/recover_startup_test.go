package workflow_test

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library/internal/coord"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/domain/series"
	"github.com/wyvernzora/kura/services/library/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

// newStartupRecoveryDeps seeds a libRoot with one series.json per refStr
// plus an indexfile.Index containing matching rows. Returns the deps,
// the parsed series refs, and the current hostname so callers can plant
// claims with a host that matches what reconcile.Recover sees at runtime.
func newStartupRecoveryDeps(t *testing.T, refStrs ...string) (workflow.Deps, []refs.Series, string) {
	t.Helper()
	root := t.TempDir()
	host, err := os.Hostname()
	if err != nil {
		t.Fatalf("Hostname: %v", err)
	}
	idx := indexfile.New(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	mut := func(op string) coord.Mutator {
		return coord.Mutator{Op: op, PID: os.Getpid(), Host: host, At: time.Now().UTC().Truncate(time.Second)}
	}
	parsed := make([]refs.Series, 0, len(refStrs))
	for i, s := range refStrs {
		ref, err := refs.ParseSeries(s)
		if err != nil {
			t.Fatalf("ParseSeries(%q): %v", s, err)
		}
		parsed = append(parsed, ref)
		meta := refs.Metadata("tvdb:" + strconv.Itoa(1000+i))
		model := &series.Series{
			Ref:      ref,
			Metadata: meta,
			Episodes: map[refs.Episode]series.Episode{},
		}
		if err := seriesfile.SaveCAS(root, model, mut("test_seed")); err != nil {
			t.Fatalf("seed series.json for %s: %v", ref, err)
		}
		if err := idx.Upsert(indexfile.Entry{Model: model}); err != nil {
			t.Fatalf("idx.Upsert(%s): %v", ref, err)
		}
	}
	deps := workflow.Deps{
		LibRoot:     root,
		Index:       idx,
		Coordinator: coord.NewCLICoordinator(),
		Now:         time.Now,
	}
	return deps, parsed, host
}

func plantStartupClaim(t *testing.T, deps workflow.Deps, ref refs.Series, holder coord.Holder) {
	t.Helper()
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load(%s): %v", ref, err)
	}
	model.InProgress = &holder
	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("test_plant")); err != nil {
		t.Fatalf("plant(%s): %v", ref, err)
	}
}

func TestRecoverStaleClaims_BreaksStaleSameHostAcrossSeries(t *testing.T) {
	deps, refsList, host := newStartupRecoveryDeps(t, "Show A", "Show B")
	for _, ref := range refsList {
		plantStartupClaim(t, deps, ref, coord.Holder{
			Op:      "reconcile_apply",
			Token:   "stale-" + ref.String(),
			PID:     0, // PID 0 → stale by IsStaleHolder
			Host:    host,
			Started: time.Now().UTC().Truncate(time.Second),
		})
	}
	out := workflow.RecoverStaleClaims(context.Background(), deps)
	if out.Scanned != 2 {
		t.Fatalf("Scanned = %d, want 2", out.Scanned)
	}
	if got := len(out.Cleared); got != 2 {
		t.Fatalf("Cleared count = %d, want 2", got)
	}
	if got := len(out.Busy); got != 0 {
		t.Fatalf("Busy count = %d, want 0", got)
	}
	for _, ref := range refsList {
		model, err := seriesfile.Load(deps.LibRoot, ref)
		if err != nil {
			t.Fatalf("Load post-recover(%s): %v", ref, err)
		}
		if model.InProgress != nil {
			t.Errorf("InProgress(%s) = %+v, want nil", ref, model.InProgress)
		}
	}
}

func TestRecoverStaleClaims_LeavesLiveClaimAsBusy(t *testing.T) {
	deps, refsList, host := newStartupRecoveryDeps(t, "Live Show")
	plantStartupClaim(t, deps, refsList[0], coord.Holder{
		Op:      "reconcile_apply",
		Token:   "live-claim",
		PID:     os.Getpid(), // current PID → live, not stale
		Host:    host,
		Started: time.Now().UTC().Truncate(time.Second),
	})
	out := workflow.RecoverStaleClaims(context.Background(), deps)
	if out.Scanned != 1 {
		t.Fatalf("Scanned = %d, want 1", out.Scanned)
	}
	if got := len(out.Cleared); got != 0 {
		t.Fatalf("Cleared = %d, want 0 (live same-host claim must not be broken)", got)
	}
	if got := len(out.Busy); got != 1 {
		t.Fatalf("Busy = %d, want 1", got)
	}
	model, err := seriesfile.Load(deps.LibRoot, refsList[0])
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if model.InProgress == nil {
		t.Fatal("InProgress = nil, want claim to still be there for the operator to inspect")
	}
}

func TestRecoverStaleClaims_LeavesCrossHostClaimAsBusy(t *testing.T) {
	deps, refsList, _ := newStartupRecoveryDeps(t, "Foreign Show")
	plantStartupClaim(t, deps, refsList[0], coord.Holder{
		Op:      "reconcile_apply",
		Token:   "cross-host",
		PID:     0, // would be stale by PID alone
		Host:    "elsewhere",
		Started: time.Now().UTC().Truncate(time.Second),
	})
	out := workflow.RecoverStaleClaims(context.Background(), deps)
	if got := len(out.Cleared); got != 0 {
		t.Errorf("Cleared = %d, want 0 (cross-host stale claims need --force)", got)
	}
	if got := len(out.Busy); got != 1 {
		t.Errorf("Busy = %d, want 1", got)
	}
}

func TestRecoverStaleClaims_NoClaimsScansAllReturnsEmpty(t *testing.T) {
	deps, _, _ := newStartupRecoveryDeps(t, "Show A", "Show B")
	out := workflow.RecoverStaleClaims(context.Background(), deps)
	if out.Scanned != 2 {
		t.Fatalf("Scanned = %d, want 2", out.Scanned)
	}
	if len(out.Cleared) != 0 || len(out.Busy) != 0 {
		t.Fatalf("Cleared=%d Busy=%d, want both 0", len(out.Cleared), len(out.Busy))
	}
}

func TestRecoverStaleClaims_HonorsCanceledContext(t *testing.T) {
	deps, _, _ := newStartupRecoveryDeps(t, "Show A", "Show B")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := workflow.RecoverStaleClaims(ctx, deps)
	if out.Scanned != 0 {
		t.Fatalf("Scanned = %d, want 0 after cancel", out.Scanned)
	}
}

func TestRecoverStaleClaims_NilIndexReturnsEmpty(t *testing.T) {
	out := workflow.RecoverStaleClaims(context.Background(), workflow.Deps{})
	if out.Scanned != 0 || len(out.Cleared) != 0 || len(out.Busy) != 0 {
		t.Fatalf("non-empty result with nil Index: %+v", out)
	}
}

package workflow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wyvernzora/kura/services/library/internal/jobs"
	"github.com/wyvernzora/kura/services/library/internal/progress"
	"github.com/wyvernzora/kura/services/library/internal/provider"
	"github.com/wyvernzora/kura/services/library/internal/response"
	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

// TestScanAll_AlwaysReturnsTrackedJob mirrors the IsTracked invariant
// test on Scan: ScanAll must always go through jobs.Submit so the REST
// transport can construct a JobAck unconditionally.
func TestScanAll_AlwaysReturnsTrackedJob(t *testing.T) {
	deps := listFixture(t)
	deps.Jobs = jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { deps.Jobs.Shutdown(0) })

	j := workflow.ScanAll(context.Background(), deps, workflow.ScanAllInput{})
	if !j.IsTracked() {
		t.Fatalf("ScanAll must always return a tracked job; got IsTracked=false")
	}
	if j.ID() == "" {
		t.Fatalf("tracked job must have non-empty ID")
	}
	if j.Kind() != string(jobs.KindScanAll) {
		t.Fatalf("Job.Kind = %q, want %q", j.Kind(), jobs.KindScanAll)
	}
}

// TestScanAll_EmptyLibrary returns Total=0 with no failures and emits
// a clean start→success progress sequence.
func TestScanAll_EmptyLibrary(t *testing.T) {
	deps := listFixture(t)
	deps.Jobs = jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { deps.Jobs.Shutdown(0) })

	ctx, events := progress.Capture(context.Background())
	j := workflow.ScanAll(ctx, deps, workflow.ScanAllInput{})
	result, err := j.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Total != 0 || result.Succeeded != 0 || result.Failed != 0 || len(result.Failures) != 0 {
		t.Fatalf("empty result = %+v, want zeroed", result)
	}
	if len(*events) < 2 {
		t.Fatalf("expected at least start+success events, got %d", len(*events))
	}
}

// TestScanAll_SkipsUntrackedScansErrors confirms ListStatusUntracked
// rows are skipped while ListStatusError rows are still scanned (the
// re-scan-fixes-error invariant).
func TestScanAll_SkipsUntrackedScansErrors(t *testing.T) {
	deps := listFixture(t,
		makeRow(t, "Tracked Complete", response.ListStatusComplete),
		makeRow(t, "Tracked Errored", response.ListStatusError),
		makeRow(t, "Untracked Dir", response.ListStatusUntracked),
	)
	deps.Jobs = jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { deps.Jobs.Shutdown(0) })

	// Provider always errors so every dispatched scan fails. We're
	// asserting which series got dispatched, not what scan does.
	deps.Provider = func() (provider.Source, error) {
		return nil, errors.New("no provider in test")
	}

	j := workflow.ScanAll(context.Background(), deps, workflow.ScanAllInput{})
	result, err := j.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("Total = %d, want 2 (untracked skipped, error included)", result.Total)
	}
	if result.Failed != 2 {
		t.Fatalf("Failed = %d, want 2 (provider always errors)", result.Failed)
	}
	if result.Succeeded != 0 {
		t.Fatalf("Succeeded = %d, want 0", result.Succeeded)
	}
	if len(result.Failures) != 2 {
		t.Fatalf("len(Failures) = %d, want 2", len(result.Failures))
	}
}

// TestScanAll_ProgressMonotonic asserts progress.current increases
// monotonically from 0 toward total without gaps.
func TestScanAll_ProgressMonotonic(t *testing.T) {
	deps := listFixture(t,
		makeRow(t, "A", response.ListStatusComplete),
		makeRow(t, "B", response.ListStatusComplete),
		makeRow(t, "C", response.ListStatusComplete),
	)
	deps.Jobs = jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { deps.Jobs.Shutdown(0) })
	deps.Provider = func() (provider.Source, error) {
		return nil, errors.New("no provider")
	}

	ctx, events := progress.Capture(context.Background())
	j := workflow.ScanAll(ctx, deps, workflow.ScanAllInput{Concurrency: 1})
	if _, err := j.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	// Filter to update events from the scan_all stage.
	var updates []progress.Event
	for _, ev := range *events {
		if ev.Stage == "scan_all" && ev.Status == progress.UpdateStatus {
			updates = append(updates, ev)
		}
	}
	if len(updates) != 3 {
		t.Fatalf("update events = %d, want 3", len(updates))
	}
	for i, ev := range updates {
		if ev.Current != i+1 {
			t.Fatalf("update %d: Current = %d, want %d", i, ev.Current, i+1)
		}
		if ev.Total != 3 {
			t.Fatalf("update %d: Total = %d, want 3", i, ev.Total)
		}
	}
}

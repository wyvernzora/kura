package jobs_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/progress"
)

func mustSeries(t *testing.T, name string) refs.Series {
	t.Helper()
	s, err := refs.ParseSeries(name)
	if err != nil {
		t.Fatalf("ParseSeries(%q): %v", name, err)
	}
	return s
}

func newTestRegistry(t *testing.T, cfg jobs.Config) *jobs.Registry {
	t.Helper()
	r := jobs.NewRegistry(context.Background(), cfg, nil)
	t.Cleanup(func() { r.Shutdown(2 * time.Second) })
	return r
}

func TestSubmit_NewJobIsTrackedAndStartsRunning(t *testing.T) {
	r := newTestRegistry(t, jobs.Config{})
	started := make(chan struct{})
	finish := make(chan struct{})
	j := jobs.Submit(r, jobs.KindScan, mustSeries(t, "show-a"), func(ctx context.Context) (int, error) {
		close(started)
		<-finish
		return 42, nil
	})
	<-started
	if !j.IsTracked() {
		t.Fatalf("Submit job must be tracked")
	}
	if j.ID() == "" {
		t.Fatalf("Submit job must have non-empty ID")
	}
	if j.Kind() != "scan" {
		t.Fatalf("Submit job Kind = %q, want %q", j.Kind(), "scan")
	}
	if j.Series() != mustSeries(t, "show-a") {
		t.Fatalf("Submit job Series = %q, want %q", j.Series(), "show-a")
	}
	if got := j.State(); got != jobs.StatusRunning {
		t.Fatalf("State = %v, want Running", got)
	}
	close(finish)
	v, err := j.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait err = %v", err)
	}
	if v != 42 {
		t.Fatalf("Wait value = %d, want 42", v)
	}
	if got := j.State(); got != jobs.StatusSucceeded {
		t.Fatalf("post-Wait state = %v, want Succeeded", got)
	}
}

func TestSubmit_FailedTerminalCarriesError(t *testing.T) {
	r := newTestRegistry(t, jobs.Config{})
	want := errors.New("boom")
	j := jobs.Submit(r, jobs.KindScan, mustSeries(t, "show-b"), func(ctx context.Context) (int, error) {
		return 0, want
	})
	_, err := j.Wait(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("Wait err = %v, want %v", err, want)
	}
	if got := j.State(); got != jobs.StatusFailed {
		t.Fatalf("State = %v, want Failed", got)
	}
}

func TestSubmit_SameKindDeDupesReturnsExistingJob(t *testing.T) {
	r := newTestRegistry(t, jobs.Config{})
	finish := make(chan struct{})
	var spawned int32
	first := jobs.Submit(r, jobs.KindScan, mustSeries(t, "show-c"), func(ctx context.Context) (int, error) {
		atomic.AddInt32(&spawned, 1)
		<-finish
		return 7, nil
	})
	second := jobs.Submit(r, jobs.KindScan, mustSeries(t, "show-c"), func(ctx context.Context) (int, error) {
		atomic.AddInt32(&spawned, 1)
		return 99, nil
	})
	if first != second {
		t.Fatalf("expected pointer equality on dedupe; first=%p second=%p", first, second)
	}
	close(finish)
	v, err := first.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait err = %v", err)
	}
	if v != 7 {
		t.Fatalf("Wait value = %d, want 7 (first goroutine's value)", v)
	}
	if n := atomic.LoadInt32(&spawned); n != 1 {
		t.Fatalf("expected exactly one goroutine to spawn; got %d", n)
	}
}

func TestSubmit_CrossKindReturnsBusyFailed(t *testing.T) {
	r := newTestRegistry(t, jobs.Config{})
	finish := make(chan struct{})
	first := jobs.Submit(r, jobs.KindScan, mustSeries(t, "show-d"), func(ctx context.Context) (int, error) {
		<-finish
		return 1, nil
	})
	second := jobs.Submit(r, jobs.KindReconcileApply, mustSeries(t, "show-d"), func(ctx context.Context) (int, error) {
		t.Fatal("cross-kind submission must not spawn a goroutine")
		return 0, nil
	})
	if second.IsTracked() {
		t.Fatalf("cross-kind submission must return untracked Failed job")
	}
	_, err := second.Wait(context.Background())
	var busy *jobs.JobBusyError
	if !errors.As(err, &busy) {
		t.Fatalf("Wait err = %v, want *JobBusyError", err)
	}
	if busy.Existing.Kind != jobs.KindScan {
		t.Fatalf("BusyError.Existing.Kind = %q, want %q", busy.Existing.Kind, jobs.KindScan)
	}
	if busy.Existing.JobID != first.ID() {
		t.Fatalf("BusyError.Existing.JobID = %q, want %q", busy.Existing.JobID, first.ID())
	}
	close(finish)
	first.Wait(context.Background())
}

func TestSubmit_AfterTerminalAllowsResubmission(t *testing.T) {
	r := newTestRegistry(t, jobs.Config{})
	first := jobs.Submit(r, jobs.KindScan, mustSeries(t, "show-e"), func(ctx context.Context) (int, error) {
		return 1, nil
	})
	first.Wait(context.Background())
	// First terminal; bySeries entry should be cleared. Second submission spawns fresh.
	second := jobs.Submit(r, jobs.KindScan, mustSeries(t, "show-e"), func(ctx context.Context) (int, error) {
		return 2, nil
	})
	if first.ID() == second.ID() {
		t.Fatalf("expected fresh ID after first terminal; got same %q", first.ID())
	}
	v, err := second.Wait(context.Background())
	if err != nil || v != 2 {
		t.Fatalf("second.Wait = (%d, %v); want (2, nil)", v, err)
	}
}

func TestSubmit_TimeoutTerminatesWithJobTimeoutError(t *testing.T) {
	r := newTestRegistry(t, jobs.Config{JobTimeout: 50 * time.Millisecond})
	j := jobs.Submit(r, jobs.KindScan, mustSeries(t, "show-f"), func(ctx context.Context) (int, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	})
	_, err := j.Wait(context.Background())
	var timeout *jobs.JobTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("Wait err = %v, want *JobTimeoutError", err)
	}
	if timeout.JobKind != jobs.KindScan {
		t.Fatalf("Timeout.JobKind = %q, want %q", timeout.JobKind, jobs.KindScan)
	}
	if timeout.Elapsed < 40*time.Millisecond {
		t.Fatalf("Timeout.Elapsed = %v, want >= 40ms", timeout.Elapsed)
	}
}

func TestSubmit_ProgressCapturedOnJob(t *testing.T) {
	r := newTestRegistry(t, jobs.Config{})
	j := jobs.Submit(r, jobs.KindScan, mustSeries(t, "show-g"), func(ctx context.Context) (int, error) {
		progress.Update(ctx, "scan", "midway", 5, 10)
		return 1, nil
	})
	j.Wait(context.Background())

	got := j.LatestProgress()
	if got == nil {
		t.Fatalf("Job.LatestProgress() == nil; want captured event")
	}
	if got.Stage != "scan" || got.Current != 5 || got.Total != 10 {
		t.Fatalf("Job.LatestProgress() = %+v; want stage=scan current=5 total=10", got)
	}

	view, err := r.Get(j.ID())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	vGot := view.Progress()
	if vGot == nil || vGot.Stage != "scan" || vGot.Current != 5 {
		t.Fatalf("UntypedJob.Progress() = %+v; want captured event", vGot)
	}
}

func TestSubmit_ProgressNotForwardedToCallerReporter(t *testing.T) {
	// Verifies the capture-only contract: a reporter installed in the
	// caller's parent ctx does NOT see job-goroutine emissions.
	// Consumers must poll Job.LatestProgress / UntypedJob.Progress.
	var seen int32
	parentCtx := progress.With(t.Context(), func(_ context.Context, _ progress.Event) {
		atomic.AddInt32(&seen, 1)
	})
	r := jobs.NewRegistry(parentCtx, jobs.Config{}, nil)
	t.Cleanup(func() { r.Shutdown(2 * time.Second) })

	j := jobs.Submit(r, jobs.KindScan, mustSeries(t, "show-i"), func(ctx context.Context) (int, error) {
		progress.Update(ctx, "scan", "midway", 1, 1)
		return 0, nil
	})
	j.Wait(context.Background())

	if got := atomic.LoadInt32(&seen); got != 0 {
		t.Fatalf("parent reporter saw %d events; capture-only contract violated", got)
	}
}

func TestShutdown_CancelsInFlightJobs(t *testing.T) {
	r := jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	started := make(chan struct{})
	done := make(chan struct{})
	jobs.Submit(r, jobs.KindScan, mustSeries(t, "show-h"), func(ctx context.Context) (int, error) {
		close(started)
		<-ctx.Done()
		close(done)
		return 0, ctx.Err()
	})
	<-started
	stuck := r.Shutdown(2 * time.Second)
	if stuck != 0 {
		t.Fatalf("Shutdown reported %d stuck jobs; want 0", stuck)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("goroutine did not observe cancel")
	}
}

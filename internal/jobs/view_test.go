package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/jobs"
)

func TestGet_NotFoundForUnknownID(t *testing.T) {
	r := newTestRegistry(t, jobs.Config{})
	_, err := r.Get("0123456789abcdef")
	var nf *jobs.JobNotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("Get err = %v, want *JobNotFoundError", err)
	}
}

func TestGet_RunningEntryReportsRunning(t *testing.T) {
	r := newTestRegistry(t, jobs.Config{})
	finish := make(chan struct{})
	j := jobs.Submit(r, jobs.KindScan, mustSeries(t, "view-a"), func(ctx context.Context) (int, error) {
		<-finish
		return 0, nil
	})
	view, err := r.Get(j.ID())
	if err != nil {
		t.Fatalf("Get err = %v", err)
	}
	if view.State() != jobs.StatusRunning {
		t.Fatalf("State = %v, want Running", view.State())
	}
	if _, ok := view.EndedAt(); ok {
		t.Fatalf("EndedAt ok=true while running")
	}
	if view.Result() != nil {
		t.Fatalf("Result non-nil while running")
	}
	if view.Err() != nil {
		t.Fatalf("Err non-nil while running")
	}
	close(finish)
	j.Wait(context.Background())
}

func TestGet_TerminalSuccessExposesEncodedResult(t *testing.T) {
	type payload struct {
		Series string `json:"series"`
		Count  int    `json:"count"`
	}
	r := newTestRegistry(t, jobs.Config{})
	want := payload{Series: "x", Count: 7}
	j := jobs.Submit(r, jobs.KindScan, mustSeries(t, "view-b"), func(ctx context.Context) (payload, error) {
		return want, nil
	})
	j.Wait(context.Background())

	view, err := r.Get(j.ID())
	if err != nil {
		t.Fatalf("Get err = %v", err)
	}
	if view.State() != jobs.StatusSucceeded {
		t.Fatalf("State = %v, want Succeeded", view.State())
	}
	endedAt, ok := view.EndedAt()
	if !ok {
		t.Fatalf("EndedAt ok=false after terminal")
	}
	if endedAt.IsZero() {
		t.Fatalf("EndedAt zero")
	}
	if view.Err() != nil {
		t.Fatalf("Err = %v on success", view.Err())
	}
	raw := view.Result()
	if raw == nil {
		t.Fatalf("Result nil on success")
	}
	var got payload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != want {
		t.Fatalf("Result decoded = %+v, want %+v", got, want)
	}
}

func TestGet_TerminalFailureExposesError(t *testing.T) {
	r := newTestRegistry(t, jobs.Config{})
	want := errors.New("boom")
	j := jobs.Submit(r, jobs.KindScan, mustSeries(t, "view-c"), func(ctx context.Context) (int, error) {
		return 0, want
	})
	j.Wait(context.Background())

	view, err := r.Get(j.ID())
	if err != nil {
		t.Fatalf("Get err = %v", err)
	}
	if view.State() != jobs.StatusFailed {
		t.Fatalf("State = %v, want Failed", view.State())
	}
	if view.Result() != nil {
		t.Fatalf("Result non-nil on failure")
	}
	if !errors.Is(view.Err(), want) {
		t.Fatalf("Err = %v, want %v", view.Err(), want)
	}
}

func TestReaper_EvictsTerminalPastRetention(t *testing.T) {
	r := newTestRegistry(t, jobs.Config{
		Retention:      30 * time.Millisecond,
		ReaperInterval: 10 * time.Millisecond,
	})
	j := jobs.Submit(r, jobs.KindScan, mustSeries(t, "view-d"), func(ctx context.Context) (int, error) {
		return 1, nil
	})
	j.Wait(context.Background())

	// Wait for reaper to evict.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := r.Get(j.ID()); err != nil {
			var nf *jobs.JobNotFoundError
			if errors.As(err, &nf) {
				return
			}
			t.Fatalf("Get err = %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("reaper did not evict terminal entry within deadline")
}

func TestReaper_PreservesRunningJobs(t *testing.T) {
	r := newTestRegistry(t, jobs.Config{
		Retention:      10 * time.Millisecond,
		ReaperInterval: 10 * time.Millisecond,
	})
	finish := make(chan struct{})
	j := jobs.Submit(r, jobs.KindScan, mustSeries(t, "view-e"), func(ctx context.Context) (int, error) {
		<-finish
		return 0, nil
	})
	time.Sleep(80 * time.Millisecond)
	if _, err := r.Get(j.ID()); err != nil {
		t.Fatalf("Get evicted running job: %v", err)
	}
	close(finish)
	j.Wait(context.Background())
}

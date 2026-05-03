package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/jobs"
)

func TestResolved_IsTrackedFalseEmptyID(t *testing.T) {
	j := jobs.Resolved(42)
	if j.IsTracked() {
		t.Fatalf("Resolved job must not be tracked")
	}
	if j.ID() != "" {
		t.Fatalf("Resolved job must have empty ID, got %q", j.ID())
	}
	if j.Kind() != "" {
		t.Fatalf("Resolved job must have empty Kind, got %q", j.Kind())
	}
}

func TestResolved_WaitReturnsValueImmediately(t *testing.T) {
	j := jobs.Resolved("hello")
	deadline := time.Now().Add(50 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	v, err := j.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait err = %v", err)
	}
	if v != "hello" {
		t.Fatalf("Wait value = %q, want %q", v, "hello")
	}
	if time.Now().After(deadline) {
		t.Fatalf("Wait blocked past deadline")
	}
}

func TestResolved_StateSucceeded(t *testing.T) {
	j := jobs.Resolved(0)
	if got, want := j.State(), jobs.StatusSucceeded; got != want {
		t.Fatalf("State = %v, want %v", got, want)
	}
}

func TestFailed_IsTrackedFalseEmptyID(t *testing.T) {
	j := jobs.Failed[int](errors.New("boom"))
	if j.IsTracked() {
		t.Fatalf("Failed job must not be tracked")
	}
	if j.ID() != "" {
		t.Fatalf("Failed job must have empty ID, got %q", j.ID())
	}
}

func TestFailed_WaitReturnsErrorImmediately(t *testing.T) {
	want := errors.New("boom")
	j := jobs.Failed[int](want)
	deadline := time.Now().Add(50 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	v, err := j.Wait(ctx)
	if !errors.Is(err, want) {
		t.Fatalf("Wait err = %v, want %v", err, want)
	}
	if v != 0 {
		t.Fatalf("Wait value = %d, want zero", v)
	}
}

func TestFailed_StateFailed(t *testing.T) {
	j := jobs.Failed[int](errors.New("boom"))
	if got, want := j.State(), jobs.StatusFailed; got != want {
		t.Fatalf("State = %v, want %v", got, want)
	}
}

func TestStatusString(t *testing.T) {
	cases := []struct {
		s    jobs.Status
		want string
	}{
		{jobs.StatusRunning, "running"},
		{jobs.StatusSucceeded, "succeeded"},
		{jobs.StatusFailed, "failed"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("Status(%d).String() = %q, want %q", c.s, got, c.want)
		}
	}
}

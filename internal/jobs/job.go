package jobs

import (
	"context"
	"sync"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
)

// Status enumerates the three states a job can be in.
type Status int

const (
	StatusRunning Status = iota
	StatusSucceeded
	StatusFailed
)

// String returns the lowercase wire form used in response.JobStatus.
func (s Status) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusSucceeded:
		return "succeeded"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Job is the result-or-handle returned by every long workflow and by
// the constructors used by short workflows. Callers either Wait for
// the typed value or read identity/state for handoff.
//
// Pre-resolved jobs (Resolved/Failed) have IsTracked()==false and
// ID()=="". Tracked jobs (Submit) have IsTracked()==true and a
// non-empty ID.
type Job[T any] struct {
	id        string
	kind      JobKind
	series    refs.Series
	startedAt time.Time
	tracked   bool

	mu       sync.RWMutex
	state    Status
	result   T
	err      error
	endedAt  time.Time
	progress *progress.Event
	doneCh   chan struct{}
}

// Wait blocks until the job is terminal or ctx is cancelled. For
// pre-resolved jobs (already terminal) Wait returns immediately.
func (j *Job[T]) Wait(ctx context.Context) (T, error) {
	select {
	case <-j.doneCh:
		j.mu.RLock()
		defer j.mu.RUnlock()
		return j.result, j.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// IsTracked reports whether the job is registered with a Registry.
// True iff the job was produced by Submit; false for Resolved/Failed.
// Implies ID() != "".
func (j *Job[T]) IsTracked() bool { return j.tracked }

// ID returns the registry-assigned ID. Empty for pre-resolved jobs.
func (j *Job[T]) ID() string { return j.id }

// Kind returns the JobKind label. Empty for pre-resolved jobs.
func (j *Job[T]) Kind() string { return string(j.kind) }

// Series returns the target series. Empty refs.Series if not
// series-scoped (no current short-workflow uses series).
func (j *Job[T]) Series() refs.Series { return j.series }

// StartedAt returns the wall-clock construction time.
func (j *Job[T]) StartedAt() time.Time { return j.startedAt }

// State returns the current job state. Snapshot only — may be stale
// immediately after returning.
func (j *Job[T]) State() Status {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.state
}

// LatestProgress returns a copy of the most recent progress event
// captured by the job goroutine, or nil if no event has been
// recorded. Surfaces (CLI spinner, MCP polling, REST SSE) call this
// to render progress; pre-resolved jobs always return nil.
func (j *Job[T]) LatestProgress() *progress.Event {
	j.mu.RLock()
	defer j.mu.RUnlock()
	if j.progress == nil {
		return nil
	}
	cp := *j.progress
	return &cp
}

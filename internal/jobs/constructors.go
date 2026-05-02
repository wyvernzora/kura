package jobs

import "time"

// Resolved returns a pre-resolved successful Job carrying v.
// IsTracked()==false; not registered with any Registry. Wait returns
// immediately. Used by short workflows that complete synchronously.
func Resolved[T any](v T) *Job[T] {
	now := time.Now()
	j := &Job[T]{
		startedAt: now,
		tracked:   false,
		state:     StatusSucceeded,
		result:    v,
		endedAt:   now,
		doneCh:    make(chan struct{}),
	}
	close(j.doneCh)
	return j
}

// Failed returns a pre-resolved failed Job carrying err.
// IsTracked()==false; not registered with any Registry. Wait returns
// immediately. Used by short workflows that fail synchronously and by
// Submit's cross-kind busy rejection path.
func Failed[T any](err error) *Job[T] {
	now := time.Now()
	j := &Job[T]{
		startedAt: now,
		tracked:   false,
		state:     StatusFailed,
		err:       err,
		endedAt:   now,
		doneCh:    make(chan struct{}),
	}
	close(j.doneCh)
	return j
}

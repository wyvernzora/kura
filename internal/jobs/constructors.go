package jobs

import "time"

func failed[T any](err error) *Job[T] {
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

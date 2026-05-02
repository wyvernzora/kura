package jobs

import (
	"encoding/json"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
)

// UntypedJob is the type-erased view of a tracked job exposed by
// Registry.Get. Polling clients (kura_job_status, future REST poll)
// read this; tools that submitted the job and want the typed result
// hold the original *Job[T] and Wait on it instead.
//
// All getters take snapshots; callers must not assume consistency
// across multiple calls. The intended use is "get the latest snapshot
// for serialization."
type UntypedJob interface {
	ID() string
	Kind() string
	Series() refs.Series
	StartedAt() time.Time
	State() Status
	// EndedAt returns the terminal timestamp; ok==false if the job is
	// still running.
	EndedAt() (time.Time, bool)
	// Progress returns a copy of the latest progress event, or nil if
	// no event has been recorded yet.
	Progress() *progress.Event
	// Result returns the JSON-encoded successful result, or nil if
	// the job is not terminal-success.
	Result() json.RawMessage
	// Err returns the workflow error, or nil if the job is not
	// terminal-failure. Includes jobs-internal errors like
	// *JobTimeoutError and the shutdown sentinel.
	Err() error
}

// entry implements UntypedJob with snapshot semantics under its own
// RWMutex.

func (e *entry) ID() string           { return e.id }
func (e *entry) Kind() string         { return string(e.kind) }
func (e *entry) Series() refs.Series  { return e.series }
func (e *entry) StartedAt() time.Time { return e.startedAt }

func (e *entry) State() Status {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
}

func (e *entry) EndedAt() (time.Time, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.state == StatusRunning {
		return time.Time{}, false
	}
	return e.endedAt, true
}

func (e *entry) Progress() *progress.Event {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.progress == nil {
		return nil
	}
	cp := *e.progress
	return &cp
}

func (e *entry) Result() json.RawMessage {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.state != StatusSucceeded {
		return nil
	}
	if e.resultJSON == nil {
		return nil
	}
	out := make(json.RawMessage, len(e.resultJSON))
	copy(out, e.resultJSON)
	return out
}

func (e *entry) Err() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.state != StatusFailed {
		return nil
	}
	return e.err
}

// Get returns the UntypedJob view of a tracked job, or
// *JobNotFoundError if the ID is unknown or evicted.
func (r *Registry) Get(id string) (UntypedJob, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byID[id]
	if !ok {
		return nil, &JobNotFoundError{JobID: id}
	}
	return e, nil
}

// IsShutdownError reports whether err is the registry's
// shutdown-cancel sentinel (kind="shutdown" in wire form).
func IsShutdownError(err error) bool {
	_, ok := err.(errShutdown)
	return ok
}

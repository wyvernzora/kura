package jobs

import (
	"fmt"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
)

// JobKind labels what a job is doing. Each kind binds to one workflow
// result type T; the binding is enforced by convention and tested via
// per-workflow IsTracked-invariant tests.
type JobKind string

const (
	KindScan           JobKind = "scan"
	KindStage          JobKind = "stage"
	KindReconcileApply JobKind = "reconcile_apply"
	KindReindex        JobKind = "reindex"
)

// JobNotFoundError indicates a Get lookup hit no entry. Either the ID
// was never valid or its terminal state was evicted past retention.
type JobNotFoundError struct {
	JobID string
}

func (e *JobNotFoundError) Error() string {
	return fmt.Sprintf("job not found: %s", e.JobID)
}

// JobTimeoutError indicates the per-job deadline (KURA_JOB_TIMEOUT)
// fired before the workflow returned.
type JobTimeoutError struct {
	JobID   string
	JobKind JobKind
	Elapsed time.Duration
}

func (e *JobTimeoutError) Error() string {
	return fmt.Sprintf("job %s (%s) timed out after %s", e.JobID, e.JobKind, e.Elapsed)
}

// JobBusyError indicates a cross-kind submission was rejected because
// a different-kind job for the same series is already running.
// Existing carries the running job's identity so the caller can
// surface it.
type JobBusyError struct {
	Series   refs.Series
	Existing BusyHandle
}

// BusyHandle is a slim view of an existing tracked job, attached to
// JobBusyError so callers can surface the live job to clients.
type BusyHandle struct {
	JobID     string
	Kind      JobKind
	Series    refs.Series
	StartedAt time.Time
}

func (e *JobBusyError) Error() string {
	return fmt.Sprintf("job for series %s busy: %s job %s already running", e.Series, e.Existing.Kind, e.Existing.JobID)
}

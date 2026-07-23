package jobs

import (
	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
)

// errkind.Coded methods on the jobs error types. See errkind.Coded
// for the interface contract.

func (e *JobNotFoundError) Kind() string     { return errkind.KindNotFound }
func (e *JobNotFoundError) Category() string { return errkind.CategoryInvalidParams }
func (e *JobNotFoundError) Data() map[string]any {
	return map[string]any{"jobId": e.JobID}
}

func (e *JobTimeoutError) Kind() string     { return errkind.KindInternal }
func (e *JobTimeoutError) Category() string { return errkind.CategoryInternalError }
func (e *JobTimeoutError) Data() map[string]any {
	return map[string]any{
		"jobId":     e.JobID,
		"kind":      string(e.JobKind),
		"elapsedMs": e.Elapsed.Milliseconds(),
	}
}

func (e *JobBusyError) Kind() string     { return errkind.KindBusy }
func (e *JobBusyError) Category() string { return errkind.CategoryInternalError }
func (e *JobBusyError) Data() map[string]any {
	return map[string]any{
		"series": e.Series.String(),
		"existing": map[string]any{
			"jobId":     e.Existing.JobID,
			"kind":      string(e.Existing.Kind),
			"series":    e.Existing.Series.String(),
			"startedAt": e.Existing.StartedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		},
	}
}

package response

import (
	"encoding/json"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
)

// JobHandle is the immediate response to a long-tool submission. The
// client uses JobID to poll JobStatus.
type JobHandle struct {
	JobID     string      `json:"jobId"`
	Kind      string      `json:"kind"`
	Series    refs.Series `json:"series"`
	StartedAt time.Time   `json:"startedAt"`
}

// JobStatus is the polled view of a tracked job. Built by the
// surface (e.g. kura_job_status tool, future REST GET /jobs/{id})
// from a jobs.UntypedJob plus the registry-side error mapping.
type JobStatus struct {
	JobID     string           `json:"jobId"`
	Kind      string           `json:"kind"`
	Series    refs.Series      `json:"series"`
	State     string           `json:"state"`
	StartedAt time.Time        `json:"startedAt"`
	EndedAt   *time.Time       `json:"endedAt,omitempty"`
	Progress  *JobProgress     `json:"progress,omitempty"`
	Result    *json.RawMessage `json:"result,omitempty"`
	Error     *JobError        `json:"error,omitempty"`
}

// JobProgress mirrors progress.Event in the wire format. Lives in
// response so surfaces can serialize it without importing
// internal/progress.
type JobProgress struct {
	Phase   string `json:"phase"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
}

// JobError is the wire form of a terminal-failed job's cause. Kind is
// a closed enum (see design/async-job.md § 10); Data carries
// kind-specific payload.
type JobError struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

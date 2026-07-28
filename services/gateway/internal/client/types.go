package client

import "encoding/json"

// The bridge declares its own wire structs rather than importing either leaf's
// pkg/api (see the package doc). Two shapes are used:
//
//   - json.RawMessage for responses that pass through to structuredContent
//     unchanged. Re-encoding through a partial struct would silently drop keys
//     a leaf added, and these bodies are forwarded, not inspected.
//   - Named structs only where the bridge actually reads fields, which is the
//     five responses §5.3 reshapes.
//
// Contract tests, not a module edge, are what keep these in step with the
// leaves.

// JobAck is the 202 body every async submission returns. The bridge reads
// jobId and drops the rest: statusUrl and streamUrl are REST affordances an
// MCP client cannot use.
type JobAck struct {
	JobID string `json:"jobId"`
}

// JobStatus is the GET /jobs/{id} body. Progress, result, and error stay raw
// so get_job can forward or drop each whole, without re-encoding a shape it
// does not otherwise read.
type JobStatus struct {
	JobID     string          `json:"jobId"`
	Kind      string          `json:"kind"`
	Ref       string          `json:"ref,omitempty"`
	State     string          `json:"state"`
	StartedAt string          `json:"startedAt"`
	EndedAt   string          `json:"endedAt,omitempty"`
	Progress  json.RawMessage `json:"progress,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
}

// ResetResult is the reset response the bridge compacts into three counts.
// A single-episode reset answers with `record`; an --all or multi-episode
// reset answers with `records`.
type ResetResult struct {
	Record       json.RawMessage   `json:"record,omitempty"`
	Records      []json.RawMessage `json:"records,omitempty"`
	TrashRemoved []string          `json:"trashRemoved,omitempty"`
	ExtraRemoved []string          `json:"extraRemoved,omitempty"`
}

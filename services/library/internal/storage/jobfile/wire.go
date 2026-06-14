// Package jobfile owns the on-disk JSONL log per job at
// <library>/.kura/jobs/<jobID>.jsonl. Each file is append-only:
//
//	line 1: header (kind, series, startedAt)
//	lines 2..N-1: progress events (status, current, total, message)
//	line N: terminal (state=succeeded with result, OR state=failed with error)
//
// Writers fsync per line — same recipe as internal/storage/planfile.Log
// — so a crash mid-write loses at most one event. Readers tolerate a
// missing terminal line (job in flight, or the writer crashed) and a
// truncated final line.
package jobfile

import "encoding/json"

// SchemaVersion is stamped into the header line. Bump when the wire
// shape changes in a way readers can't tolerate.
const SchemaVersion = 1

// Line types. The `type` discriminator field on each line lets a
// streaming reader fan out into the typed shape without loading the
// whole file.
const (
	LineHeader   = "header"
	LineProgress = "progress"
	LineTerminal = "terminal"
)

// HeaderLine is the first record in every job log. SeriesRef is the
// unprefixed series reference (e.g. "Anime/Frieren") or "" for
// library-wide jobs (reindex / scan_all).
type HeaderLine struct {
	Type          string `json:"type"`
	SchemaVersion int    `json:"schemaVersion"`
	JobID         string `json:"jobId"`
	Kind          string `json:"kind"`
	SeriesRef     string `json:"series,omitempty"`
	StartedAt     string `json:"startedAt"`
}

// ProgressLine mirrors progress.Event, with `type` and `at` added.
type ProgressLine struct {
	Type    string `json:"type"`
	At      string `json:"at"`
	Stage   string `json:"stage,omitempty"`
	Status  string `json:"status,omitempty"`
	Current int    `json:"current,omitempty"`
	Total   int    `json:"total,omitempty"`
	Message string `json:"message,omitempty"`
}

// TerminalLine carries the closing record. State matches
// jobs.Status.String() ("succeeded" | "failed"). Result is the
// workflow's marshalled result (raw JSON so this package stays
// kind-agnostic); failed jobs may carry partial-progress detail
// there. On failure, Error carries the projected errkind envelope.
type TerminalLine struct {
	Type   string          `json:"type"`
	At     string          `json:"at"`
	State  string          `json:"state"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *TerminalError  `json:"error,omitempty"`
}

// TerminalError mirrors the errkind envelope kura already speaks on
// every transport.
type TerminalError struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

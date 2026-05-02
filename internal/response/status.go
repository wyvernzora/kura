// Package response holds the canonical JSON shapes that workflow
// functions return. CLI --json output and MCP tool responses share these
// shapes. Response files contain shape + simple constructors only; no
// derivation logic, no IO. Derivation (computeStatus, computeIssues) lives
// in internal/workflow/ as unexported helpers.
package response

// Status is the observed state of one episode at the time a read
// workflow ran. Mirrors Product.md § "Episode State (Observed)."
type Status string

const (
	// StatusPending: episode air date is in the future and no media is
	// recorded.
	StatusPending Status = "pending"

	// StatusMissing: episode air date has passed and no media is
	// recorded.
	StatusMissing Status = "missing"

	// StatusPresent: episode has an active media record and the file is
	// reachable on disk.
	StatusPresent Status = "present"

	// StatusStaged: episode has a staged media record awaiting
	// reconcile (no active record present).
	StatusStaged Status = "staged"

	// StatusStagedReplacement: episode has both an active record and a
	// staged record; reconcile will replace the active one.
	StatusStagedReplacement Status = "staged_replacement"

	// StatusUnavailable: episode has an active media record but the
	// file is missing or unreadable on disk.
	StatusUnavailable Status = "unavailable"
)

// Issue describes a filesystem inconsistency observed while reading a
// series (e.g. tracked file is missing, path escapes the series root).
type Issue struct {
	Record string `json:"record"`
	Path   string `json:"path"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

// Package api defines the public wire contracts shared by library-manager
// workflows, serve transports, and external clients. Wire-contract files
// contain shapes and simple constructors only; no derivation logic or IO.
// Derivation (computeStatus, computeIssues) lives in internal/workflow/ as
// unexported helpers.
package api

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
	// reconcile. Whether an active record is also present — i.e. whether
	// reconcile will replace a file or just re-record facts about one —
	// is visible in the episode's records, not in its status.
	StatusStaged Status = "staged"
)

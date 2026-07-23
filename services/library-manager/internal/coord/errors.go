package coord

import (
	"fmt"
	"time"
)

// BusyError is returned when a workflow attempts to mutate a series
// (or the library index) while another holder owns the in_progress
// claim. The Holder identifies the owner; surfaces use it for both
// CLI messages and MCP error data.
type BusyError struct {
	Scope  string
	Holder Holder
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("%s busy: %s on host=%s pid=%d since %s ago",
		e.Scope, e.Holder.Op, e.Holder.Host, e.Holder.PID,
		time.Since(e.Holder.Started).Round(time.Second))
}

// ConflictError is returned when a CAS write detects that the file
// changed between load and write. Phase distinguishes pre_write
// (drift detected before rename) from post_write (our rename was
// clobbered before the verify read).
type ConflictError struct {
	Scope   string
	Phase   string
	Mutator Mutator
}

func (e *ConflictError) Error() string {
	if e.Mutator.Op == "" {
		return fmt.Sprintf("%s conflict at %s", e.Scope, e.Phase)
	}
	return fmt.Sprintf("%s conflict at %s: lost race to %s from pid=%d host=%s at %s",
		e.Scope, e.Phase, e.Mutator.Op, e.Mutator.PID, e.Mutator.Host,
		e.Mutator.At.Format(time.RFC3339))
}

// ClaimStolenError is returned when a claim-holding workflow finds
// that its claim was cleared or overwritten between acquisition and
// the final write. Side effects already ran; the workflow surfaces
// this error rather than retrying.
type ClaimStolenError struct {
	Scope    string
	Expected Holder
	Found    *Holder
}

func (e *ClaimStolenError) Error() string {
	if e.Found == nil {
		return fmt.Sprintf("%s claim stolen: expected pid=%d host=%s, found cleared",
			e.Scope, e.Expected.PID, e.Expected.Host)
	}
	return fmt.Sprintf("%s claim stolen: expected pid=%d host=%s, found pid=%d host=%s",
		e.Scope, e.Expected.PID, e.Expected.Host, e.Found.PID, e.Found.Host)
}

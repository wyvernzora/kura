package reconcile

import (
	"fmt"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
)

// PlanAlreadyAppliedError signals the plan log already has a success
// record. Apply refuses to re-apply.
type PlanAlreadyAppliedError struct {
	Token string
}

func (e *PlanAlreadyAppliedError) Error() string {
	return fmt.Sprintf("reconcile: plan %s was already applied", e.Token)
}

// InProgressError signals an apply is already running for the same plan
// token (same caller retry, or duplicate request). The Holder identifies
// the original apply.
type InProgressError struct {
	Token  string
	Holder coord.Holder
}

func (e *InProgressError) Error() string {
	return fmt.Sprintf("reconcile: apply for token %s is already in progress on host=%s pid=%d since %s",
		e.Token, e.Holder.Host, e.Holder.PID, e.Holder.Started.Format(time.RFC3339))
}

// ApplyStepError wraps a per-step execution failure with the failing
// step's identity. Caller can errors.As against this type to surface
// "which step blew up" without round-tripping through ApplyResult.
type ApplyStepError struct {
	StepID    string
	StepKind  StepKind
	OwnerKind OwnerKind
	From      string
	To        string
	Path      string
	Err       error
}

func (e *ApplyStepError) Error() string {
	target := e.Path
	if target == "" {
		target = e.From + " -> " + e.To
	}
	return fmt.Sprintf("reconcile: step %s (%s, %s, %s): %v", e.StepID, e.StepKind, e.OwnerKind, target, e.Err)
}

func (e *ApplyStepError) Unwrap() error { return e.Err }

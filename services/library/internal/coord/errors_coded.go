package coord

import (
	"github.com/wyvernzora/kura/services/library/internal/errkind"
)

// errkind.Coded methods on the coord error types. See errkind.Coded
// for the interface contract; the workflow + jobs packages provide
// matching methods for their own error types.

func (e *BusyError) Kind() string     { return errkind.KindBusy }
func (e *BusyError) Category() string { return errkind.CategoryInternalError }
func (e *BusyError) Data() map[string]any {
	return map[string]any{
		"scope":  e.Scope,
		"holder": HolderData(e.Holder),
	}
}

func (e *ConflictError) Kind() string     { return errkind.KindConflict }
func (e *ConflictError) Category() string { return errkind.CategoryInternalError }
func (e *ConflictError) Data() map[string]any {
	out := map[string]any{
		"scope": e.Scope,
		"phase": e.Phase,
	}
	if e.Mutator.Op != "" {
		out["mutator"] = MutatorData(e.Mutator)
	}
	return out
}

func (e *ClaimStolenError) Kind() string     { return errkind.KindClaimStolen }
func (e *ClaimStolenError) Category() string { return errkind.CategoryInternalError }
func (e *ClaimStolenError) Data() map[string]any {
	out := map[string]any{
		"scope":    e.Scope,
		"expected": HolderData(e.Expected),
	}
	if e.Found != nil {
		out["found"] = HolderData(*e.Found)
	}
	return out
}

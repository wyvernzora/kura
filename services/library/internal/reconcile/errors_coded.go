package reconcile

import (
	"github.com/wyvernzora/kura/services/library/internal/coord"
	"github.com/wyvernzora/kura/services/library/internal/errkind"
)

func (e *PlanAlreadyAppliedError) Kind() string     { return errkind.KindPlanApplied }
func (e *PlanAlreadyAppliedError) Category() string { return errkind.CategoryInternalError }
func (e *PlanAlreadyAppliedError) Data() map[string]any {
	return map[string]any{"token": e.Token}
}

func (e *InProgressError) Kind() string     { return errkind.KindBusy }
func (e *InProgressError) Category() string { return errkind.CategoryInternalError }
func (e *InProgressError) Data() map[string]any {
	return map[string]any{
		"token":  e.Token,
		"holder": coord.HolderData(e.Holder),
	}
}

func (e *ApplyStepError) Kind() string     { return errkind.KindApplyStepFailed }
func (e *ApplyStepError) Category() string { return errkind.CategoryInternalError }
func (e *ApplyStepError) Data() map[string]any {
	out := map[string]any{
		"stepID":    e.StepID,
		"kind":      string(e.StepKind),
		"ownerKind": string(e.OwnerKind),
	}
	if e.Path != "" {
		out["path"] = e.Path
	}
	if e.From != "" {
		out["from"] = e.From
	}
	if e.To != "" {
		out["to"] = e.To
	}
	return out
}

package workflow

import (
	"context"
	"errors"
	"io/fs"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/reconcile"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/planfile"
)

// ApplyReconcileResponse projects a reconcile.ApplyResult into the
// response shape served by every surface (CLI / MCP). Populated even
// when the apply errored so callers see partial-progress alongside the
// failure detail.
//
// Trash-step destinations are redacted: the on-disk `to` path inside
// .kura/trash/<ulid>/ is operator territory (visible in the apply log
// on disk), not part of the wire contract. The original `from` path
// is preserved so callers know which file failed to land in trash.
func ApplyReconcileResponse(result reconcile.ApplyResult) response.ReconcileApply {
	out := response.ReconcileApply{
		Series:         result.Series,
		AppliedSteps:   result.AppliedSteps,
		TotalSteps:     result.TotalSteps,
		AppliedStepIDs: append([]string(nil), result.AppliedStepIDs...),
	}
	if result.FailedStep != nil {
		fs := &response.FailedReconcileStep{
			ID:         result.FailedStep.ID,
			Kind:       string(result.FailedStep.Kind),
			OwnerKind:  string(result.FailedStep.OwnerKind),
			From:       result.FailedStep.From,
			To:         result.FailedStep.To,
			Path:       result.FailedStep.Path,
			ErrMessage: result.FailedStep.ErrMessage,
		}
		if result.FailedStep.OwnerKind == reconcile.OwnerTrash {
			fs.To = ""
		}
		out.FailedStep = fs
	}
	return out
}

// ApplyReconcileInput parameters for the ApplyReconcile workflow.
type ApplyReconcileInput struct {
	Ref   refs.Series
	Token string
}

// ApplyReconcile loads the persisted plan, opens the apply log, and
// dispatches to reconcile.Apply. Always returns a tracked *jobs.Job;
// plan-load failures surface inside the goroutine as terminal errors.
func ApplyReconcile(ctx context.Context, deps Deps, in ApplyReconcileInput) *jobs.Job[reconcile.ApplyResult] {
	rd := reconcileDeps(deps)
	return jobs.Submit(deps.Jobs, ctx, jobs.KindReconcileApply, in.Ref, func(jobCtx context.Context) (reconcile.ApplyResult, error) {
		plan, applied, err := planfile.ReadPlan(deps.LibRoot, in.Ref, in.Token)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return reconcile.ApplyResult{}, &ReconcilePlanNotFoundError{Ref: in.Ref, Token: in.Token}
			}
			return reconcile.ApplyResult{}, err
		}
		if applied {
			return reconcile.ApplyResult{}, &reconcile.PlanAlreadyAppliedError{Token: in.Token}
		}
		log, err := planfile.OpenLog(deps.LibRoot, in.Ref, in.Token)
		if err != nil {
			return reconcile.ApplyResult{}, err
		}
		return reconcile.Apply(jobCtx, rd, reconcile.ApplyInput{
			Ref:     in.Ref,
			Plan:    plan,
			Log:     log,
			LogStop: log.Close,
		})
	})
}

package workflow

import (
	"context"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// RecoverReconcileInput parameters for the RecoverReconcile workflow.
// Force=true breaks the claim regardless of holder identity (the
// cross-host stale escape hatch). Without Force the workflow only
// clears same-host stale claims; live or cross-host claims surface
// as BusyError.
type RecoverReconcileInput struct {
	Ref   refs.Series
	Force bool
}

// RecoverReconcile clears the in_progress claim on a series.json. Used
// when a prior reconcile apply died mid-flight without releasing its
// claim and same-host PID detection cannot break it (cross-host crash,
// PID-wrap collision, etc.).
//
// No-op when no claim is set. Returns BusyError when the holder is
// alive same-host or cross-host (without Force).
func RecoverReconcile(ctx context.Context, deps Deps, in RecoverReconcileInput) (response.RecoverReconcile, error) {
	_ = ctx
	var out response.RecoverReconcile
	out.Ref = in.Ref
	err := deps.Coordinator.WithSeries(in.Ref, func() error {
		model, err := seriesfile.Load(deps.LibRoot, in.Ref)
		if err != nil {
			return err
		}
		if model.InProgress == nil {
			out.Cleared = false
			return nil
		}
		holder := *model.InProgress
		out.PriorHolder = &holder
		if !in.Force {
			if !coord.IsStaleHolder(holder) {
				return &coord.BusyError{Scope: coord.SeriesScope(in.Ref), Holder: holder}
			}
		}
		model.InProgress = nil
		if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("reconcile_recover")); err != nil {
			return err
		}
		out.Cleared = true
		return nil
	})
	return out, err
}

package reconcile

import (
	"context"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// RecoverInput parameters for Recover. Force=true breaks the claim
// regardless of holder identity (cross-host stale escape hatch).
// Without Force the workflow only clears same-host stale claims; live
// or cross-host claims surface as BusyError.
type RecoverInput struct {
	Ref   refs.Series
	Force bool
}

// RecoverResult records what Recover did. Cleared is true when an
// in_progress claim was actually removed; PriorHolder identifies who
// was holding it.
type RecoverResult struct {
	Ref         refs.Series
	Cleared     bool
	PriorHolder *coord.Holder
}

// Recover clears the in_progress claim on a series.json. Used when a
// prior reconcile apply died mid-flight without releasing its claim and
// same-host PID detection cannot break it (cross-host crash, PID-wrap
// collision, etc.).
//
// No-op when no claim is set. Returns BusyError when the holder is
// alive same-host or cross-host (without Force).
func Recover(ctx context.Context, deps Deps, in RecoverInput) (RecoverResult, error) {
	out := RecoverResult{Ref: in.Ref}
	log := deps.log().With("ref", in.Ref.String())
	err := deps.Coordinator.WithSeries(ctx, in.Ref, func() error {
		model, err := seriesfile.Load(deps.LibRoot, in.Ref)
		if err != nil {
			return err
		}
		if model.InProgress == nil {
			log.Info("recover no claim to clear")
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
		log.Info("recover cleared claim",
			"priorHolder", holder.Op,
			"host", holder.Host,
			"pid", holder.PID,
			"force", in.Force,
		)
		return nil
	})
	return out, err
}

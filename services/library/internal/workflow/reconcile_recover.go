package workflow

import (
	"context"

	"github.com/wyvernzora/kura/services/library/internal/reconcile"
	"github.com/wyvernzora/kura/services/library/internal/response"
)

// RecoverReconcileInput parameters for the RecoverReconcile workflow.
type RecoverReconcileInput = reconcile.RecoverInput

// RecoverReconcile clears the in_progress claim on a series.json. Used
// when a prior reconcile apply died mid-flight without releasing its
// claim and same-host PID detection cannot break it.
//
// Force=true breaks the claim regardless of holder identity. Without
// Force the workflow only clears same-host stale claims; live or
// cross-host claims surface as BusyError.
func RecoverReconcile(ctx context.Context, deps Deps, in RecoverReconcileInput) (response.RecoverReconcile, error) {
	out, err := reconcile.Recover(ctx, reconcileDeps(deps), in)
	if err != nil {
		return response.RecoverReconcile{Ref: in.Ref}, err
	}
	return response.RecoverReconcile{
		Ref:         out.Ref,
		Cleared:     out.Cleared,
		PriorHolder: out.PriorHolder,
	}, nil
}

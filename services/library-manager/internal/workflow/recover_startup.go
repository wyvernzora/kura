package workflow

import (
	"context"
	"errors"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/reconcile"
)

// StartupRecoveryResult summarizes a startup recovery sweep across the
// library index. Cleared lists series whose stale same-host claim was
// removed; Busy lists series whose claim was still alive (live process
// or cross-host) and could not be safely cleared without --force.
type StartupRecoveryResult struct {
	Scanned int
	Cleared []reconcile.RecoverResult
	Busy    []*coord.BusyError
}

// RecoverStaleClaims iterates every row in the live index and runs
// reconcile.Recover (without Force) against each one. The intent is to
// auto-clear claims left over from a previous server instance that
// died mid-flight: the new instance shares the same KURA_HOST_ID
// stamp, so coord.IsStaleHolder can detect the dead PID and free the
// claim without operator intervention.
//
// Cross-host stale claims and live same-host claims are reported as
// Busy but not cleared — those still need a deliberate
// `kura reconcile recover --force` because Kura cannot safely
// distinguish "previous container, now gone" from "concurrent writer
// on a peer node" without operator knowledge.
//
// Returns no error: per-series failures are aggregated into the
// result. Callers (cmd_serve at boot) call this once after the index
// is ready and log the summary. Honors ctx cancellation between rows.
func RecoverStaleClaims(ctx context.Context, deps Deps) StartupRecoveryResult {
	out := StartupRecoveryResult{}
	if deps.Index == nil {
		return out
	}
	rdeps := reconcileDeps(deps)
	for _, row := range deps.Index.Rows() {
		select {
		case <-ctx.Done():
			return out
		default:
		}
		out.Scanned++
		result, err := reconcile.Recover(ctx, rdeps, reconcile.RecoverInput{Ref: row.Series})
		if err != nil {
			var busy *coord.BusyError
			if errors.As(err, &busy) {
				out.Busy = append(out.Busy, busy)
				continue
			}
			if deps.Logger != nil {
				deps.Logger.Warn("startup recovery: unexpected error",
					"ref", row.Series.String(),
					"err", err,
				)
			}
			continue
		}
		if result.Cleared {
			out.Cleared = append(out.Cleared, result)
		}
	}
	return out
}

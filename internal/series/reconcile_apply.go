package series

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/wyvernzora/kura/internal/progress"
)

func (h Handle) ApplyReconcile(ctx context.Context, plan ReconcilePlan) (ReconcileResult, error) {
	progress.Start(ctx, "reconcile", fmt.Sprintf("Applying reconcile for %s", h.ref), 0)
	if err := h.validatePlan(plan); err != nil {
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", h.ref), 0, 0)
		return ReconcileResult{}, err
	}
	if !plan.HasChanges() {
		progress.Success(ctx, "reconcile", fmt.Sprintf("Reconciled %s", h.ref), 0)
		return ReconcileResult{Series: h.ref}, nil
	}
	seriesDir, err := h.files().seriesDir(h.ref)
	if err != nil {
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", h.ref), 0, 0)
		return ReconcileResult{}, err
	}
	moves := plan.Moves()
	for index, move := range moves {
		progress.Update(ctx, "reconcile", fmt.Sprintf("Moving %s", filepath.Base(move.To)), index+1, len(moves))
		from := move.From
		if !filepath.IsAbs(from) {
			from = filepath.Join(seriesDir.Path(), filepath.FromSlash(move.From))
		}
		to := filepath.Join(seriesDir.Path(), filepath.FromSlash(move.To))
		if err := h.files().move(from, to); err != nil {
			progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", h.ref), index+1, len(moves))
			return ReconcileResult{}, err
		}
	}
	updated, err := h.applyPlanState(plan)
	if err != nil {
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", h.ref), len(moves), len(moves))
		return ReconcileResult{}, err
	}
	if err := h.repo().save(h.ref, updated); err != nil {
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", h.ref), len(moves), len(moves))
		return ReconcileResult{}, err
	}
	progress.Success(ctx, "reconcile", fmt.Sprintf("Reconciled %s", h.ref), len(moves))
	return ReconcileResult{Series: h.ref, AppliedMoves: len(moves)}, nil
}

func (h Handle) validatePlan(plan ReconcilePlan) error {
	if plan.Series != h.ref {
		return PlanStaleError{Series: plan.Series}
	}
	snapshot, err := h.snapshot()
	if err != nil {
		return err
	}
	if snapshot != plan.Snapshot {
		return PlanStaleError{Series: plan.Series}
	}
	return nil
}

func (h Handle) applyPlanState(plan ReconcilePlan) (seriesState, error) {
	series, err := h.load()
	if err != nil {
		return seriesState{}, err
	}
	edit := editor{series: &series}
	for _, change := range plan.Changes {
		episode := series.Episodes[change.Episode]
		switch change.Kind {
		case ChangeAdd, ChangeReplace:
			if episode.Staged == nil {
				return seriesState{}, fmt.Errorf("series: %s has no staged media", change.Episode)
			}
			if change.Replaced != nil && episode.Active != nil {
				if err := h.writeTrash(change.Episode, *episode.Active, *change.Replaced); err != nil {
					return seriesState{}, err
				}
			}
			episode.Staged.Path = change.To
			for index := range episode.Staged.Companions {
				if index < len(change.Companions) {
					episode.Staged.Companions[index].Path = change.Companions[index].To
				}
			}
			series.Episodes[change.Episode] = episode
			if _, err := edit.promoteStaged(change.Episode); err != nil {
				return seriesState{}, err
			}
		case ChangeMove:
			if episode.Active == nil {
				return seriesState{}, fmt.Errorf("series: %s has no active media", change.Episode)
			}
			episode.Active.Path = change.To
			for index := range episode.Active.Companions {
				if index < len(change.Companions) {
					episode.Active.Companions[index].Path = change.Companions[index].To
				}
			}
			series.Episodes[change.Episode] = episode
		default:
			return seriesState{}, fmt.Errorf("series: unsupported reconcile change kind %q", change.Kind)
		}
	}
	return series, nil
}

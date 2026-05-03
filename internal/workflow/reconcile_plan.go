package workflow

import (
	"context"
	"errors"
	"io/fs"
	"time"

	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/reconcile"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/planfile"
)

// PlanReconcileTTL is how long a persisted plan remains valid for apply.
const PlanReconcileTTL = reconcile.PlanTTL

// PlanReconcileInput parameters for the PlanReconcile workflow.
type PlanReconcileInput = reconcile.PlanInput

// PlanReconcile loads the series state, builds the unrolled v2 step
// plan via the reconcile package, and persists it under
// <series>/.kura/reconcile/<token>.jsonl when there is work to do.
// Empty plans return without writing.
//
// Token derivation is deterministic: identical series state always
// produces the same token, so this entry point is idempotent at the
// file level (existing planfile is returned as-is).
func PlanReconcile(ctx context.Context, deps Deps, in PlanReconcileInput) (response.ReconcilePlan, error) {
	plan, err := reconcile.BuildPlan(ctx, reconcileDeps(deps), in)
	if err != nil {
		return response.ReconcilePlan{}, err
	}
	out := response.ReconcilePlan{Plan: planToResponse(plan)}
	if !plan.HasWork() {
		return out, nil
	}

	// Idempotency: if a planfile already exists for this token, return
	// it as-is. Apply re-validates the snapshot at execute time.
	if existing, _, err := planfile.ReadPlan(deps.LibRoot, in.Ref, plan.Header.Token); err == nil {
		out.Token = existing.Header.Token
		ca := existing.Header.CreatedAt
		ea := existing.Header.ExpiresAt
		out.CreatedAt = &ca
		out.ExpiresAt = &ea
		out.Plan = planToResponse(existing)
		return out, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return response.ReconcilePlan{}, err
	}

	plan.Header.CreatedAt = deps.Now().UTC().Truncate(time.Second)
	plan.Header.ExpiresAt = plan.Header.CreatedAt.Add(reconcile.PlanTTL)
	if err := planfile.WritePlan(deps.LibRoot, in.Ref, plan); err != nil {
		return response.ReconcilePlan{}, err
	}
	out.Token = plan.Header.Token
	ca := plan.Header.CreatedAt
	ea := plan.Header.ExpiresAt
	out.CreatedAt = &ca
	out.ExpiresAt = &ea
	out.Plan = planToResponse(plan)
	return out, nil
}

// reconcileDeps builds a reconcile.Deps from a workflow.Deps. Binds
// UpdateIndex to the workflow-side updateIndexRow so reconcile can
// invoke index-CAS without importing workflow.
func reconcileDeps(deps Deps) reconcile.Deps {
	rd := reconcile.Deps{
		LibRoot:     deps.LibRoot,
		InboxRoot:   deps.InboxRoot,
		Now:         deps.Now,
		Coordinator: deps.Coordinator,
		Logger:      deps.Logger,
		Index:       deps.Index,
		Jobs:        deps.Jobs,
	}
	rd.UpdateIndex = func(ctx context.Context, model *domainseries.Series, op string) error {
		return updateIndexRow(ctx, deps, model, op)
	}
	return rd
}

// planToResponse projects a v2 Plan into the response shape that CLI /
// MCP renderers consume. The projection groups steps by Owner and
// emits a per-grouping entry that carries the step IDs that produced
// it, so callers can deterministically map response entries back to
// plan steps.
//
// Replaced-active trash steps (OwnerTrash with OriginalEpisode set)
// are pre-collected into ReconcileReplaced entries and attached to the
// triggering episode change — they do NOT appear in TrashItems.
// Standalone stagedTrash (no OriginalEpisode) populates TrashItems.
//
// Within an Owner group the first encountered step is the primary
// (drives From/To and the entry's media facts via Owner.StagedRecord
// or Owner.Record); subsequent steps in the same group become
// companion moves. Plan steps are emitted in episode-ref-sorted order
// by the planner so first-encounter order is deterministic.
func planToResponse(plan reconcile.Plan) response.ReconcilePlanDetail {
	out := response.ReconcilePlanDetail{Series: plan.Header.Series}

	replacedByEpisode := map[string]*response.ReconcileReplaced{}
	for _, step := range plan.Steps {
		if step.Owner.Kind != reconcile.OwnerTrash || step.Owner.OriginalEpisode.IsZero() {
			continue
		}
		key := step.Owner.OriginalEpisode.String()
		rep, ok := replacedByEpisode[key]
		if !ok {
			rep = &response.ReconcileReplaced{From: step.From, To: step.To}
			if rec := step.Owner.Record; rec != nil {
				rep.Source = rec.Source
				rep.Resolution = rec.Resolution
				rep.Codec = rec.Codec
				rep.Size = rec.Size
				if !rec.MTime.IsZero() {
					mt := rec.MTime
					rep.MTime = &mt
				}
			}
			replacedByEpisode[key] = rep
		} else if step.Kind == reconcile.StepFileMove {
			rep.Companions = append(rep.Companions, response.ReconcileMove{From: step.From, To: step.To})
		}
		rep.StepIDs = append(rep.StepIDs, step.ID)
	}

	episodes := map[string]*response.ReconcileChange{}
	trash := map[string]*response.ReconcileTrashChange{}
	extras := map[string]*response.ReconcileExtraChange{}

	for _, step := range plan.Steps {
		switch step.Owner.Kind {
		case reconcile.OwnerEpisode:
			key := step.Owner.EpisodeRef.String()
			entry, ok := episodes[key]
			if !ok {
				entry = &response.ReconcileChange{
					Kind:     step.Owner.EpisodeIntent,
					Episode:  step.Owner.EpisodeRef,
					From:     step.From,
					To:       step.To,
					Replaced: replacedByEpisode[key],
				}
				if rec := step.Owner.StagedRecord; rec != nil {
					entry.Source = rec.Source
					entry.Resolution = rec.Resolution
					entry.Codec = rec.Codec
					entry.Size = rec.Size
					if !rec.MTime.IsZero() {
						mt := rec.MTime
						entry.MTime = &mt
					}
				}
				episodes[key] = entry
			} else if step.Kind == reconcile.StepFileMove {
				entry.Companions = append(entry.Companions, response.ReconcileMove{From: step.From, To: step.To})
			}
			entry.StepIDs = append(entry.StepIDs, step.ID)
		case reconcile.OwnerTrash:
			if !step.Owner.OriginalEpisode.IsZero() {
				continue // already projected into ReconcileChange.Replaced
			}
			key := step.Owner.TrashID
			entry, ok := trash[key]
			if !ok {
				entry = &response.ReconcileTrashChange{ID: key, From: step.From, To: step.To}
				if rec := step.Owner.Record; rec != nil {
					entry.Source = rec.Source
					entry.Resolution = rec.Resolution
					entry.Codec = rec.Codec
					entry.Size = rec.Size
					if !rec.MTime.IsZero() {
						mt := rec.MTime
						entry.MTime = &mt
					}
				}
				trash[key] = entry
			} else if step.Kind == reconcile.StepFileMove {
				entry.Companions = append(entry.Companions, response.ReconcileMove{From: step.From, To: step.To})
			}
			entry.StepIDs = append(entry.StepIDs, step.ID)
		case reconcile.OwnerExtra:
			key := step.Owner.ExtraID
			entry, ok := extras[key]
			if !ok {
				entry = &response.ReconcileExtraChange{
					ID:     key,
					Season: step.Owner.Season,
					Prefix: step.Owner.Prefix,
				}
				extras[key] = entry
			}
			entry.StepIDs = append(entry.StepIDs, step.ID)
			if entry.From == "" && step.Kind == reconcile.StepFileMove {
				entry.From = step.From
				entry.To = step.To
			}
			// Any dir_remove step under this extras owner means the
			// source was a directory tree.
			if step.Kind == reconcile.StepDirRemove {
				entry.IsDir = true
			}
		}
	}

	// Emit grouped entries in first-encounter order over the (already
	// sorted) plan steps. Map iteration would lose that ordering.
	seenEp := map[string]bool{}
	seenTr := map[string]bool{}
	seenEx := map[string]bool{}
	for _, step := range plan.Steps {
		switch step.Owner.Kind {
		case reconcile.OwnerEpisode:
			key := step.Owner.EpisodeRef.String()
			if seenEp[key] {
				continue
			}
			seenEp[key] = true
			out.Changes = append(out.Changes, *episodes[key])
		case reconcile.OwnerTrash:
			if !step.Owner.OriginalEpisode.IsZero() {
				continue
			}
			key := step.Owner.TrashID
			if seenTr[key] {
				continue
			}
			seenTr[key] = true
			out.TrashItems = append(out.TrashItems, *trash[key])
		case reconcile.OwnerExtra:
			key := step.Owner.ExtraID
			if seenEx[key] {
				continue
			}
			seenEx[key] = true
			out.Extras = append(out.Extras, *extras[key])
		}
	}
	return out
}

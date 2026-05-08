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
	replaced := buildReplacedIndex(plan.Steps)
	episodes, trash, extras := groupPlanSteps(plan.Steps, replaced)
	emitGroupedChanges(&out, plan.Steps, episodes, trash, extras)
	return out
}

// applyMediaFacts copies the media-fact fields from rec into the
// caller's response entry. Each of the three response shapes
// (ReconcileReplaced / ReconcileChange / ReconcileTrashChange) carries
// the same Source/Resolution/Codec/Size/MTime quintet, so the inner
// nil-check + MTime.IsZero() shape is hoisted here once instead of
// repeated three times across the grouping passes.
func applyMediaFacts(rec *reconcile.ReplacedRecord, source, resolution, codec *string, size *int64, mtime **time.Time) {
	if rec == nil {
		return
	}
	*source = rec.Source
	*resolution = rec.Resolution
	*codec = rec.Codec
	*size = rec.Size
	if !rec.MTime.IsZero() {
		mt := rec.MTime
		*mtime = &mt
	}
}

// buildReplacedIndex walks plan.Steps and gathers the
// "trash step that replaces an active episode" entries keyed by the
// episode they replace. groupPlanSteps consumes the result to attach
// each ReconcileReplaced onto the triggering ReconcileChange.
func buildReplacedIndex(steps []reconcile.Step) map[string]*response.ReconcileReplaced {
	out := map[string]*response.ReconcileReplaced{}
	for _, step := range steps {
		if step.Owner.Kind != reconcile.OwnerTrash || step.Owner.OriginalEpisode.IsZero() {
			continue
		}
		key := step.Owner.OriginalEpisode.String()
		rep, ok := out[key]
		if !ok {
			rep = &response.ReconcileReplaced{From: step.From, To: step.To}
			applyMediaFacts(step.Owner.Record, &rep.Source, &rep.Resolution, &rep.Codec, &rep.Size, &rep.MTime)
			out[key] = rep
		} else if step.Kind == reconcile.StepFileMove {
			rep.Companions = append(rep.Companions, response.ReconcileMove{From: step.From, To: step.To})
		}
		rep.StepIDs = append(rep.StepIDs, step.ID)
	}
	return out
}

// groupPlanSteps walks plan.Steps a second time and accumulates
// per-Owner change maps. Each Owner kind dispatches to a small
// per-kind helper so the parent stays a 3-arm switch.
func groupPlanSteps(
	steps []reconcile.Step,
	replaced map[string]*response.ReconcileReplaced,
) (
	episodes map[string]*response.ReconcileChange,
	trash map[string]*response.ReconcileTrashChange,
	extras map[string]*response.ReconcileExtraChange,
) {
	episodes = map[string]*response.ReconcileChange{}
	trash = map[string]*response.ReconcileTrashChange{}
	extras = map[string]*response.ReconcileExtraChange{}
	for _, step := range steps {
		switch step.Owner.Kind {
		case reconcile.OwnerEpisode:
			groupEpisodeStep(episodes, replaced, step)
		case reconcile.OwnerTrash:
			groupTrashStep(trash, step)
		case reconcile.OwnerExtra:
			groupExtraStep(extras, step)
		}
	}
	return episodes, trash, extras
}

func groupEpisodeStep(
	episodes map[string]*response.ReconcileChange,
	replaced map[string]*response.ReconcileReplaced,
	step reconcile.Step,
) {
	key := step.Owner.EpisodeRef.String()
	entry, ok := episodes[key]
	if !ok {
		entry = &response.ReconcileChange{
			Kind:     step.Owner.EpisodeIntent,
			Episode:  step.Owner.EpisodeRef,
			From:     step.From,
			To:       step.To,
			Replaced: replaced[key],
		}
		applyMediaFacts(step.Owner.StagedRecord, &entry.Source, &entry.Resolution, &entry.Codec, &entry.Size, &entry.MTime)
		episodes[key] = entry
	} else if step.Kind == reconcile.StepFileMove {
		entry.Companions = append(entry.Companions, response.ReconcileMove{From: step.From, To: step.To})
	}
	entry.StepIDs = append(entry.StepIDs, step.ID)
}

func groupTrashStep(
	trash map[string]*response.ReconcileTrashChange,
	step reconcile.Step,
) {
	if !step.Owner.OriginalEpisode.IsZero() {
		// Already projected into ReconcileChange.Replaced.
		return
	}
	key := step.Owner.TrashID
	entry, ok := trash[key]
	if !ok {
		entry = &response.ReconcileTrashChange{ID: key, From: step.From, To: step.To}
		applyMediaFacts(step.Owner.Record, &entry.Source, &entry.Resolution, &entry.Codec, &entry.Size, &entry.MTime)
		trash[key] = entry
	} else if step.Kind == reconcile.StepFileMove {
		entry.Companions = append(entry.Companions, response.ReconcileMove{From: step.From, To: step.To})
	}
	entry.StepIDs = append(entry.StepIDs, step.ID)
}

func groupExtraStep(
	extras map[string]*response.ReconcileExtraChange,
	step reconcile.Step,
) {
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
	// Any dir_remove step under this extras owner means the source
	// was a directory tree.
	if step.Kind == reconcile.StepDirRemove {
		entry.IsDir = true
	}
}

// emitGroupedChanges walks plan.Steps a third time to flush the
// grouped per-Owner maps into out in deterministic first-encounter
// order. Map iteration would lose that ordering.
func emitGroupedChanges(
	out *response.ReconcilePlanDetail,
	steps []reconcile.Step,
	episodes map[string]*response.ReconcileChange,
	trash map[string]*response.ReconcileTrashChange,
	extras map[string]*response.ReconcileExtraChange,
) {
	seenEp := map[string]bool{}
	seenTr := map[string]bool{}
	seenEx := map[string]bool{}
	for _, step := range steps {
		switch step.Owner.Kind {
		case reconcile.OwnerEpisode:
			key := step.Owner.EpisodeRef.String()
			if !seenEp[key] {
				seenEp[key] = true
				out.Changes = append(out.Changes, *episodes[key])
			}
		case reconcile.OwnerTrash:
			if step.Owner.OriginalEpisode.IsZero() {
				key := step.Owner.TrashID
				if !seenTr[key] {
					seenTr[key] = true
					out.TrashItems = append(out.TrashItems, *trash[key])
				}
			}
		case reconcile.OwnerExtra:
			key := step.Owner.ExtraID
			if !seenEx[key] {
				seenEx[key] = true
				out.Extras = append(out.Extras, *extras[key])
			}
		}
	}
}

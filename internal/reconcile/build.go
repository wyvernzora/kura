package reconcile

import (
	"context"
	"fmt"
	"os"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesdir"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// TokenLength is the canonical length of a reconcile plan token: a
// 12-char lowercase hex prefix of the snapshot sha256.
const TokenLength = 12

// PlanInput parameters for BuildPlan.
type PlanInput struct {
	Ref refs.Series
}

// BuildPlan loads the series state and constructs the unrolled v2 step
// plan. Persistence is the caller's responsibility (the workflow shim
// wires the plan to planfile.WritePlan).
//
// Plan token = snapshot[:TokenLength]; deterministic given the series
// state. Apply re-validates the snapshot at execute time, so the token
// alone is the freshness check — there is no separate TTL.
//
// Returned plan has zero CreatedAt; the caller stamps it at
// persistence time.
func BuildPlan(ctx context.Context, deps Deps, in PlanInput) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}

	model, err := seriesfile.Load(deps.LibRoot, in.Ref)
	if err != nil {
		return Plan{}, err
	}
	if model.InProgress != nil {
		return Plan{}, &coord.BusyError{Scope: coord.SeriesScope(in.Ref), Holder: *model.InProgress}
	}
	rawSeries, err := os.ReadFile(paths.SeriesMetadata(deps.LibRoot, in.Ref))
	if err != nil {
		return Plan{}, err
	}
	seriesDir, err := seriesdir.Parse(paths.SeriesDir(deps.LibRoot, in.Ref))
	if err != nil {
		return Plan{}, err
	}

	snapshot := Snapshot(rawSeries)
	token := snapshot[:TokenLength]

	log := deps.log().With("ref", in.Ref.String(), "token", token)
	var steps []Step
	// Standalone stagedTrash runs first so trash-bucket moves clear the
	// active layout before any episode or extras step writes to it. An
	// episode canonicalization whose target path is currently occupied
	// by a user-staged trash file would otherwise clobber the trash file
	// when apply walks steps in plan order.
	trashSteps, err := planTrash(token, seriesDir.Path(), model)
	if err != nil {
		return Plan{}, err
	}
	steps = append(steps, trashSteps...)
	log.Debug("plan: trash", "steps", len(trashSteps))
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	episodeSteps, err := planEpisodes(token, in.Ref, seriesDir.Path(), model)
	if err != nil {
		return Plan{}, err
	}
	steps = append(steps, episodeSteps...)
	log.Debug("plan: episodes", "steps", len(episodeSteps))
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	extraSteps, err := planExtras(token, model, deps.InboxRoot)
	if err != nil {
		return Plan{}, err
	}
	steps = append(steps, extraSteps...)
	log.Debug("plan: extras", "steps", len(extraSteps))

	if err := validateSteps(steps); err != nil {
		return Plan{}, err
	}

	log.Info("plan built",
		"totalSteps", len(steps),
		"episodeSteps", len(episodeSteps),
		"trashSteps", len(trashSteps),
		"extraSteps", len(extraSteps),
	)

	return Plan{
		Header: Header{
			SchemaVersion: 2,
			Token:         token,
			Series:        in.Ref,
			Snapshot:      snapshot,
		},
		Steps: steps,
	}, nil
}

// validateSteps catches collisions: two steps with different sources
// targeting the same destination.
func validateSteps(steps []Step) error {
	targets := map[string]string{}
	for _, s := range steps {
		if s.Kind != StepFileMove {
			continue
		}
		if s.From == s.To {
			continue
		}
		if existing, exists := targets[s.To]; exists && existing != s.From {
			return fmt.Errorf("reconcile: multiple steps target %q", s.To)
		}
		targets[s.To] = s.From
	}
	return nil
}

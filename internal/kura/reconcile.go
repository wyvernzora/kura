package kura

import (
	"context"
	"errors"

	"github.com/wyvernzora/kura/internal/refs"
	seriespkg "github.com/wyvernzora/kura/internal/series"
)

func (s *Series) PlanReconcile(_ context.Context, _ ReconcileInput) (ReconcilePlan, error) {
	handle, err := s.library.series.Open(refs.Series(s.ref))
	if err != nil {
		return ReconcilePlan{}, normalizeSeriesLibraryError(err)
	}
	plan, err := handle.PlanReconcile()
	if err != nil {
		return ReconcilePlan{}, normalizeSeriesLibraryError(err)
	}
	return reconcilePlanFromSeries(plan), nil
}

func (s *Series) ApplyReconcile(_ context.Context, plan ReconcilePlan) (ReconcileResult, error) {
	handle, err := s.library.series.Open(refs.Series(s.ref))
	if err != nil {
		return ReconcileResult{}, normalizeSeriesLibraryError(err)
	}
	result, err := handle.ApplyReconcile(reconcilePlanToSeries(plan))
	if err != nil {
		var stale seriespkg.PlanStaleError
		if errors.As(err, &stale) {
			return ReconcileResult{}, PlanStaleError{Series: SeriesRef(stale.Series)}
		}
		return ReconcileResult{}, normalizeSeriesLibraryError(err)
	}
	model, loadErr := handle.Load()
	if loadErr == nil {
		s.model = model
	}
	return ReconcileResult{Series: SeriesRef(result.Series), AppliedMoves: result.AppliedMoves}, nil
}

func reconcilePlanFromSeries(plan seriespkg.ReconcilePlan) ReconcilePlan {
	out := ReconcilePlan{
		Series:    SeriesRef(plan.Series),
		FileTitle: plan.FileTitle,
		Snapshot:  plan.Snapshot,
		Changes:   make([]Change, 0, len(plan.Changes)),
	}
	for _, change := range plan.Changes {
		out.Changes = append(out.Changes, changeFromSeries(change))
	}
	return out
}

func reconcilePlanToSeries(plan ReconcilePlan) seriespkg.ReconcilePlan {
	out := seriespkg.ReconcilePlan{
		Series:    refs.Series(plan.Series),
		FileTitle: plan.FileTitle,
		Snapshot:  plan.Snapshot,
		Changes:   make([]seriespkg.Change, 0, len(plan.Changes)),
	}
	for _, change := range plan.Changes {
		out.Changes = append(out.Changes, changeToSeries(change))
	}
	return out
}

func changeFromSeries(change seriespkg.Change) Change {
	out := Change{
		Kind:     ChangeKind(change.Kind),
		Season:   change.Episode.Season(),
		Episode:  change.Episode.Episode(),
		FileMove: FileMove{From: change.From, To: change.To},

		Source:     change.Source,
		Resolution: change.Resolution,
		Companions: fileMovesFromSeries(change.Companions),
	}
	if change.Replaced != nil {
		out.Replaced = &Replaced{
			FileMove:   FileMove{From: change.Replaced.From, To: change.Replaced.To},
			Source:     change.Replaced.Source,
			Resolution: change.Replaced.Resolution,
			Companions: fileMovesFromSeries(change.Replaced.Companions),
		}
	}
	return out
}

func changeToSeries(change Change) seriespkg.Change {
	episode, _ := refs.NewEpisode(change.Season, change.Episode)
	out := seriespkg.Change{
		Kind:       seriespkg.ChangeKind(change.Kind),
		Episode:    episode,
		FileMove:   seriespkg.FileMove{From: change.From, To: change.To},
		Source:     change.Source,
		Resolution: change.Resolution,
		Companions: fileMovesToSeries(change.Companions),
	}
	if change.Replaced != nil {
		out.Replaced = &seriespkg.Replaced{
			FileMove:   seriespkg.FileMove{From: change.Replaced.From, To: change.Replaced.To},
			Source:     change.Replaced.Source,
			Resolution: change.Replaced.Resolution,
			Companions: fileMovesToSeries(change.Replaced.Companions),
		}
	}
	return out
}

func fileMovesFromSeries(in []seriespkg.FileMove) []FileMove {
	out := make([]FileMove, 0, len(in))
	for _, move := range in {
		out = append(out, FileMove{From: move.From, To: move.To})
	}
	return out
}

func fileMovesToSeries(in []FileMove) []seriespkg.FileMove {
	out := make([]seriespkg.FileMove, 0, len(in))
	for _, move := range in {
		out = append(out, seriespkg.FileMove{From: move.From, To: move.To})
	}
	return out
}

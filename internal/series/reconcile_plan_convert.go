package series

import (
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series/wire"
	"github.com/wyvernzora/kura/internal/textnorm"
)

func toWireReconcilePlan(plan ReconcilePlan) wire.ReconcilePlanV1 {
	out := wire.ReconcilePlanV1{
		Series:    plan.Series.String(),
		FileTitle: plan.FileTitle.String(),
		Snapshot:  plan.Snapshot,
		Changes:   make([]wire.ReconcileChangeV1, 0, len(plan.Changes)),
	}
	for _, change := range plan.Changes {
		out.Changes = append(out.Changes, toWireReconcileChange(change))
	}
	return out
}

func toWireReconcileChange(change Change) wire.ReconcileChangeV1 {
	out := wire.ReconcileChangeV1{
		Kind:       string(change.Kind),
		Episode:    change.Episode.String(),
		From:       change.From,
		To:         change.To,
		Source:     change.Source,
		Resolution: change.Resolution,
		Companions: toWireReconcileMoves(change.Companions),
	}
	if change.Replaced != nil {
		out.Replaced = &wire.ReconcileReplacedV1{
			From:       change.Replaced.From,
			To:         change.Replaced.To,
			Source:     change.Replaced.Source,
			Resolution: change.Replaced.Resolution,
			Companions: toWireReconcileMoves(change.Replaced.Companions),
		}
	}
	return out
}

func toWireReconcileMoves(moves []FileMove) []wire.ReconcileFileMoveV1 {
	if len(moves) == 0 {
		return nil
	}
	out := make([]wire.ReconcileFileMoveV1, 0, len(moves))
	for _, move := range moves {
		out = append(out, wire.ReconcileFileMoveV1{From: move.From, To: move.To})
	}
	return out
}

func fromWireReconcilePlan(in wire.ReconcilePlanV1) (ReconcilePlan, error) {
	seriesRef, err := refs.ParseSeries(in.Series)
	if err != nil {
		return ReconcilePlan{}, err
	}
	out := ReconcilePlan{
		Series:    seriesRef,
		FileTitle: textnorm.NFC(in.FileTitle),
		Snapshot:  in.Snapshot,
		Changes:   make([]Change, 0, len(in.Changes)),
	}
	for _, change := range in.Changes {
		converted, err := fromWireReconcileChange(change)
		if err != nil {
			return ReconcilePlan{}, err
		}
		out.Changes = append(out.Changes, converted)
	}
	return out, nil
}

func fromWireReconcileChange(in wire.ReconcileChangeV1) (Change, error) {
	episode, err := refs.ParseEpisode(in.Episode)
	if err != nil {
		return Change{}, err
	}
	out := Change{
		Kind:       ChangeKind(in.Kind),
		Episode:    episode,
		FileMove:   FileMove{From: in.From, To: in.To},
		Source:     in.Source,
		Resolution: in.Resolution,
		Companions: fromWireReconcileMoves(in.Companions),
	}
	if in.Replaced != nil {
		out.Replaced = &Replaced{
			FileMove:   FileMove{From: in.Replaced.From, To: in.Replaced.To},
			Source:     in.Replaced.Source,
			Resolution: in.Replaced.Resolution,
			Companions: fromWireReconcileMoves(in.Replaced.Companions),
		}
	}
	return out, nil
}

func fromWireReconcileMoves(in []wire.ReconcileFileMoveV1) []FileMove {
	if len(in) == 0 {
		return nil
	}
	out := make([]FileMove, 0, len(in))
	for _, move := range in {
		out = append(out, FileMove{From: move.From, To: move.To})
	}
	return out
}

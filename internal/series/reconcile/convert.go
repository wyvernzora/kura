package reconcile

import (
	"github.com/wyvernzora/kura/internal/storage/planfile"
	"github.com/wyvernzora/kura/internal/textnorm"
)

func toPlanFileChanges(in []Change) []planfile.Change {
	out := make([]planfile.Change, 0, len(in))
	for _, change := range in {
		w := planfile.Change{
			Kind:       string(change.Kind),
			Episode:    change.Episode,
			From:       change.From,
			To:         change.To,
			Source:     change.Source,
			Resolution: change.Resolution,
			Companions: toPlanFileMoves(change.Companions),
		}
		if change.Replaced != nil {
			w.Replaced = &planfile.Replaced{
				From:       change.Replaced.From,
				To:         change.Replaced.To,
				Source:     change.Replaced.Source,
				Resolution: change.Replaced.Resolution,
				Companions: toPlanFileMoves(change.Replaced.Companions),
			}
		}
		out = append(out, w)
	}
	return out
}

func toPlanFileMoves(in []FileMove) []planfile.FileMove {
	if len(in) == 0 {
		return nil
	}
	out := make([]planfile.FileMove, 0, len(in))
	for _, m := range in {
		out = append(out, planfile.FileMove{From: m.From, To: m.To})
	}
	return out
}

func fromPlanFileRecord(in planfile.PlanRecord) ReconcilePlan {
	return ReconcilePlan{
		Series:    in.Series,
		FileTitle: textnorm.NFC(in.FileTitle.String()),
		Snapshot:  in.Snapshot,
		Changes:   fromPlanFileChanges(in.Changes),
	}
}

func fromPlanFileChanges(in []planfile.Change) []Change {
	out := make([]Change, 0, len(in))
	for _, w := range in {
		change := Change{
			Kind:       ChangeKind(w.Kind),
			Episode:    w.Episode,
			FileMove:   FileMove{From: w.From, To: w.To},
			Source:     w.Source,
			Resolution: w.Resolution,
			Companions: fromPlanFileMoves(w.Companions),
		}
		if w.Replaced != nil {
			change.Replaced = &Replaced{
				FileMove:   FileMove{From: w.Replaced.From, To: w.Replaced.To},
				Source:     w.Replaced.Source,
				Resolution: w.Replaced.Resolution,
				Companions: fromPlanFileMoves(w.Replaced.Companions),
			}
		}
		out = append(out, change)
	}
	return out
}

func fromPlanFileMoves(in []planfile.FileMove) []FileMove {
	if len(in) == 0 {
		return nil
	}
	out := make([]FileMove, 0, len(in))
	for _, m := range in {
		out = append(out, FileMove{From: m.From, To: m.To})
	}
	return out
}

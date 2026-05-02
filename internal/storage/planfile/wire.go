package planfile

import (
	"fmt"
	"time"

	"github.com/wyvernzora/kura/internal/domain/reconcile"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

type planRecordV1 struct {
	Type          string `json:"type"`
	SchemaVersion int    `json:"schemaVersion"`
	Token         string `json:"token"`
	CreatedAt     string `json:"createdAt"`
	ExpiresAt     string `json:"expiresAt"`
	Plan          planV1 `json:"plan"`
}

type planV1 struct {
	Series   string     `json:"series"`
	Snapshot string     `json:"snapshot"`
	Changes  []changeV1 `json:"changes"`
}

type changeV1 struct {
	Kind       string       `json:"kind"`
	Episode    string       `json:"episode"`
	From       string       `json:"from"`
	To         string       `json:"to"`
	Source     string       `json:"source,omitempty"`
	Resolution string       `json:"resolution,omitempty"`
	Companions []fileMoveV1 `json:"companions,omitempty"`
	Replaced   *replacedV1  `json:"replaced,omitempty"`
}

type fileMoveV1 struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type replacedV1 struct {
	From       string       `json:"from"`
	To         string       `json:"to"`
	Source     string       `json:"source,omitempty"`
	Resolution string       `json:"resolution,omitempty"`
	Companions []fileMoveV1 `json:"companions,omitempty"`
}

type eventV1 struct {
	Type          string     `json:"type"`
	SchemaVersion int        `json:"schemaVersion"`
	At            string     `json:"at"`
	Index         int        `json:"index"`
	Total         int        `json:"total"`
	Move          fileMoveV1 `json:"move"`
	Error         string     `json:"error,omitempty"`
}

type resultV1 struct {
	Type          string `json:"type"`
	SchemaVersion int    `json:"schemaVersion"`
	At            string `json:"at"`
	Status        string `json:"status"`
	AppliedMoves  int    `json:"appliedMoves,omitempty"`
	Error         string `json:"error,omitempty"`
}

func planRecordToWire(p PlanRecord) planRecordV1 {
	return planRecordV1{
		Type:          "plan",
		SchemaVersion: currentSchemaVersion,
		Token:         p.Token,
		CreatedAt:     p.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:     p.ExpiresAt.UTC().Format(time.RFC3339),
		Plan: planV1{
			Series:   p.Plan.Series.String(),
			Snapshot: p.Plan.Snapshot,
			Changes:  changesToWire(p.Plan.Changes),
		},
	}
}

func changesToWire(in []reconcile.Change) []changeV1 {
	out := make([]changeV1, 0, len(in))
	for _, change := range in {
		w := changeV1{
			Kind:       string(change.Kind),
			Episode:    change.Episode.String(),
			From:       change.From,
			To:         change.To,
			Source:     change.Source,
			Resolution: change.Resolution,
			Companions: fileMovesToWire(change.Companions),
		}
		if change.Replaced != nil {
			w.Replaced = &replacedV1{
				From:       change.Replaced.From,
				To:         change.Replaced.To,
				Source:     change.Replaced.Source,
				Resolution: change.Replaced.Resolution,
				Companions: fileMovesToWire(change.Replaced.Companions),
			}
		}
		out = append(out, w)
	}
	return out
}

func fileMovesToWire(in []reconcile.FileMove) []fileMoveV1 {
	if len(in) == 0 {
		return nil
	}
	out := make([]fileMoveV1, 0, len(in))
	for _, m := range in {
		out = append(out, fileMoveV1{From: m.From, To: m.To})
	}
	return out
}

func fileMoveToWire(in reconcile.FileMove) fileMoveV1 {
	return fileMoveV1{From: in.From, To: in.To}
}

func planRecordFromWire(in planRecordV1) (PlanRecord, error) {
	createdAt, err := time.Parse(time.RFC3339, in.CreatedAt)
	if err != nil {
		return PlanRecord{}, fmt.Errorf("planfile: invalid createdAt %q: %w", in.CreatedAt, err)
	}
	expiresAt, err := time.Parse(time.RFC3339, in.ExpiresAt)
	if err != nil {
		return PlanRecord{}, fmt.Errorf("planfile: invalid expiresAt %q: %w", in.ExpiresAt, err)
	}
	seriesRef, err := refs.ParseSeries(in.Plan.Series)
	if err != nil {
		return PlanRecord{}, err
	}
	changes, err := changesFromWire(in.Plan.Changes)
	if err != nil {
		return PlanRecord{}, err
	}
	return PlanRecord{
		Token:     in.Token,
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
		Plan: reconcile.Plan{
			Series:   seriesRef,
			Snapshot: in.Plan.Snapshot,
			Changes:  changes,
		},
	}, nil
}

func changesFromWire(in []changeV1) ([]reconcile.Change, error) {
	out := make([]reconcile.Change, 0, len(in))
	for _, w := range in {
		episode, err := refs.ParseEpisode(w.Episode)
		if err != nil {
			return nil, err
		}
		change := reconcile.Change{
			Kind:       reconcile.ChangeKind(w.Kind),
			Episode:    episode,
			FileMove:   reconcile.FileMove{From: w.From, To: w.To},
			Source:     w.Source,
			Resolution: w.Resolution,
			Companions: fileMovesFromWire(w.Companions),
		}
		if w.Replaced != nil {
			change.Replaced = &reconcile.Replaced{
				FileMove:   reconcile.FileMove{From: w.Replaced.From, To: w.Replaced.To},
				Source:     w.Replaced.Source,
				Resolution: w.Replaced.Resolution,
				Companions: fileMovesFromWire(w.Replaced.Companions),
			}
		}
		out = append(out, change)
	}
	return out, nil
}

func fileMovesFromWire(in []fileMoveV1) []reconcile.FileMove {
	if len(in) == 0 {
		return nil
	}
	out := make([]reconcile.FileMove, 0, len(in))
	for _, m := range in {
		out = append(out, reconcile.FileMove{From: m.From, To: m.To})
	}
	return out
}

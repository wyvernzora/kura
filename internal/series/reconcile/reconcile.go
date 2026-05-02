package reconcile

import (
	"fmt"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/textnorm"
)

type ReconcilePlan struct {
	Series    refs.Series        `json:"series"`
	FileTitle textnorm.NFCString `json:"fileTitle"`
	Snapshot  string             `json:"snapshot"`
	Changes   []Change           `json:"changes"`
}

func (p ReconcilePlan) HasChanges() bool {
	return len(p.Changes) > 0
}

func (p ReconcilePlan) Moves() []FileMove {
	var moves []FileMove
	for _, change := range p.Changes {
		moves = append(moves, change.Moves()...)
	}
	return moves
}

type FileMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Change struct {
	Kind    ChangeKind   `json:"kind"`
	Episode refs.Episode `json:"episode"`
	FileMove
	Source     string     `json:"source,omitempty"`
	Resolution string     `json:"resolution,omitempty"`
	Companions []FileMove `json:"companions,omitempty"`
	Replaced   *Replaced  `json:"replaced,omitempty"`
}

func (c Change) Moves() []FileMove {
	moves := make([]FileMove, 0, 2+len(c.Companions))
	if c.Replaced != nil {
		moves = append(moves, c.Replaced.FileMove)
		moves = append(moves, c.Replaced.Companions...)
	}
	moves = append(moves, c.FileMove)
	moves = append(moves, c.Companions...)
	return moves
}

type ChangeKind string

const (
	ChangeAdd     ChangeKind = "add"
	ChangeMove    ChangeKind = "move"
	ChangeReplace ChangeKind = "replace"
)

type Replaced struct {
	FileMove
	Source     string     `json:"source,omitempty"`
	Resolution string     `json:"resolution,omitempty"`
	Companions []FileMove `json:"companions,omitempty"`
}

type ReconcileResult struct {
	Series       refs.Series `json:"series"`
	AppliedMoves int         `json:"appliedMoves"`
}

type PlanStaleError struct {
	Series refs.Series
}

func (err PlanStaleError) Error() string {
	return fmt.Sprintf("series: reconcile plan for %s is stale", err.Series)
}

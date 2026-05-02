// Package reconcile defines the immutable plan and per-change types that
// describe what a reconcile would do. Pure types and pure helpers; no IO.
//
// Persistence lives in internal/storage/planfile. Composition (loading the
// series, scanning the filesystem, building the change list) lives in
// internal/workflow.
package reconcile

import (
	"github.com/wyvernzora/kura/internal/domain/refs"
)

// Plan captures the immutable intent of a reconcile: which series, the
// snapshot of its on-disk metadata at plan time, and the ordered changes to
// apply.
type Plan struct {
	Series   refs.Series
	Snapshot string
	Changes  []Change
}

// HasChanges reports whether the plan contains any work.
func (p Plan) HasChanges() bool {
	return len(p.Changes) > 0
}

// Moves flattens the plan into the ordered file moves apply will execute.
// Replaced (trash) moves precede the change's primary move; companion moves
// follow.
func (p Plan) Moves() []FileMove {
	var moves []FileMove
	for _, change := range p.Changes {
		moves = append(moves, change.Moves()...)
	}
	return moves
}

// Change is one episode-level transition.
type Change struct {
	Kind    ChangeKind
	Episode refs.Episode
	FileMove
	Source     string
	Resolution string
	Companions []FileMove
	Replaced   *Replaced
}

// Moves returns the ordered file moves required for this change. Replaced
// moves run first (so trash is in place before staging takes the slot).
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

// ChangeKind discriminates Change variants.
type ChangeKind string

const (
	ChangeAdd     ChangeKind = "add"
	ChangeMove    ChangeKind = "move"
	ChangeReplace ChangeKind = "replace"
)

// FileMove records the source and destination of one filesystem move. Paths
// are series-dir-relative slash-form strings.
type FileMove struct {
	From string
	To   string
}

// Replaced captures the active record being displaced when a stage replaces
// existing media. The FileMove records the active media path and its trash
// destination.
type Replaced struct {
	FileMove
	Source     string
	Resolution string
	Companions []FileMove
}

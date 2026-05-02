package reconcile

import (
	domainreconcile "github.com/wyvernzora/kura/internal/domain/reconcile"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

type ReconcilePlan = domainreconcile.Plan

type FileMove = domainreconcile.FileMove

type Change = domainreconcile.Change

type ChangeKind = domainreconcile.ChangeKind

const (
	ChangeAdd     = domainreconcile.ChangeAdd
	ChangeMove    = domainreconcile.ChangeMove
	ChangeReplace = domainreconcile.ChangeReplace
)

type Replaced = domainreconcile.Replaced

type ReconcileResult struct {
	Series       refs.Series `json:"series"`
	AppliedMoves int         `json:"appliedMoves"`
}

type PlanStaleError = domainreconcile.StaleSnapshotError

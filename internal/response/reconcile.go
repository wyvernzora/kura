package response

import (
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

// ReconcilePlan is workflow.PlanReconcile's response. Token/CreatedAt/
// ExpiresAt are zero when the plan has no changes and no file was
// persisted.
type ReconcilePlan struct {
	Token     string              `json:"token,omitempty"`
	CreatedAt *time.Time          `json:"createdAt,omitempty"`
	ExpiresAt *time.Time          `json:"expiresAt,omitempty"`
	Plan      ReconcilePlanDetail `json:"plan"`
}

// ReconcilePlanDetail is the on-disk plan as seen by callers. Snapshot is
// omitted from the JSON shape; apply re-reads the on-disk record.
type ReconcilePlanDetail struct {
	Series  refs.Series       `json:"series"`
	Changes []ReconcileChange `json:"changes"`
}

type ReconcileChange struct {
	Kind       string             `json:"kind"`
	Episode    refs.Episode       `json:"episode"`
	From       string             `json:"from"`
	To         string             `json:"to"`
	Source     string             `json:"source,omitempty"`
	Resolution string             `json:"resolution,omitempty"`
	Companions []ReconcileMove    `json:"companions,omitempty"`
	Replaced   *ReconcileReplaced `json:"replaced,omitempty"`
}

type ReconcileMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type ReconcileReplaced struct {
	From       string          `json:"from"`
	To         string          `json:"to"`
	Source     string          `json:"source,omitempty"`
	Resolution string          `json:"resolution,omitempty"`
	Companions []ReconcileMove `json:"companions,omitempty"`
}

// ReconcileApply is workflow.ApplyReconcile's response.
type ReconcileApply struct {
	Series       refs.Series `json:"series"`
	AppliedMoves int         `json:"appliedMoves"`
}

// RecoverReconcile is workflow.RecoverReconcile's response. Cleared is
// true when an in_progress claim was actually removed; PriorHolder
// identifies who was holding it.
type RecoverReconcile struct {
	Ref         refs.Series   `json:"ref"`
	Cleared     bool          `json:"cleared"`
	PriorHolder *coord.Holder `json:"priorHolder,omitempty"`
}

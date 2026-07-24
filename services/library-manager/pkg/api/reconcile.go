package api

import (
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
)

// ReconcilePlan is workflow.PlanReconcile's response. Token / CreatedAt
// are zero when the plan has no changes and no file was persisted.
// There is no expiry: apply re-validates the snapshot at execute time,
// and a stale plan (series state changed) is rejected by token
// mismatch.
type ReconcilePlan struct {
	Token     string              `json:"token,omitempty"`
	CreatedAt *time.Time          `json:"createdAt,omitempty"`
	Plan      ReconcilePlanDetail `json:"plan"`
}

// ReconcilePlanDetail is the on-disk plan as seen by callers. Snapshot is
// omitted from the JSON shape; apply re-reads the on-disk record.
type ReconcilePlanDetail struct {
	Series     refs.Series            `json:"series"`
	Changes    []ReconcileChange      `json:"changes"`
	TrashItems []ReconcileTrashChange `json:"trashItems,omitempty"`
	Extras     []ReconcileExtraChange `json:"extras,omitempty"`
}

// ReconcileTrashChange is one stagedTrash item that will move to
// .kura/trash/<id>/ on apply. StepIDs lists the step IDs that produced
// this trash bucket so callers can deterministically map the entry
// back to plan steps. Source / Resolution / Codec are empty for
// standalone stagedTrash (no mediainfo probe at stage time); Size and
// MTime are populated from the on-disk file.
type ReconcileTrashChange struct {
	ID         string          `json:"id"`
	From       string          `json:"from"`
	To         string          `json:"to"`
	Source     string          `json:"source,omitempty"`
	Resolution string          `json:"resolution,omitempty"`
	Codec      string          `json:"codec,omitempty"`
	Size       int64           `json:"size,omitempty"`
	MTime      *time.Time      `json:"mtime,omitempty"`
	Companions []ReconcileMove `json:"companions,omitempty"`
	StepIDs    []string        `json:"stepIds,omitempty"`
}

// ReconcileExtraChange is one stagedExtras item that will move into
// Season N/Extra/[prefix]/<basename> on apply.
type ReconcileExtraChange struct {
	ID      string   `json:"id"`
	Season  int      `json:"season"`
	From    string   `json:"from"`
	To      string   `json:"to"`
	Prefix  string   `json:"prefix,omitempty"`
	IsDir   bool     `json:"isDir,omitempty"`
	StepIDs []string `json:"stepIds,omitempty"`
}

type ReconcileChange struct {
	Kind       string             `json:"kind"`
	Episode    refs.Episode       `json:"episode"`
	From       string             `json:"from"`
	To         string             `json:"to"`
	Source     string             `json:"source,omitempty"`
	Resolution string             `json:"resolution,omitempty"`
	Codec      string             `json:"codec,omitempty"`
	Size       int64              `json:"size,omitempty"`
	MTime      *time.Time         `json:"mtime,omitempty"`
	Companions []ReconcileMove    `json:"companions,omitempty"`
	Replaced   *ReconcileReplaced `json:"replaced,omitempty"`
	StepIDs    []string           `json:"stepIds,omitempty"`
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
	Codec      string          `json:"codec,omitempty"`
	Size       int64           `json:"size,omitempty"`
	MTime      *time.Time      `json:"mtime,omitempty"`
	Companions []ReconcileMove `json:"companions,omitempty"`
	StepIDs    []string        `json:"stepIds,omitempty"`
}

// ReconcileApply is workflow.ApplyReconcile's response. Populated on
// both success and failure: AppliedSteps + AppliedStepIDs reflect what
// the apply actually moved (may be < TotalSteps on partial failure),
// FailedStep is non-nil when a per-step execution failure aborted the
// run. Pre-flight failures (snapshot stale, claim contention) leave
// FailedStep nil — those did not touch any step.
//
// On partial failure, series.json reflects the pre-apply model — staged
// records remain staged, active records remain active, no trash drain.
// Operator's recovery path is `kura reconcile recover` + `kura scan`.
type ReconcileApply struct {
	Series         refs.Series          `json:"series"`
	AppliedSteps   int                  `json:"appliedSteps"`
	TotalSteps     int                  `json:"totalSteps"`
	AppliedStepIDs []string             `json:"appliedStepIds,omitempty"`
	FailedStep     *FailedReconcileStep `json:"failedStep,omitempty"`
}

// FailedReconcileStep names the step whose execution failed during
// apply. Path / From / To are scheme-tagged selectors
// (`series:<rel>` for in-library moves, `inbox:<rel>` for inbox
// sources).
type FailedReconcileStep struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	OwnerKind  string `json:"ownerKind"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	Path       string `json:"path,omitempty"`
	ErrMessage string `json:"error"`
}

// RecoverReconcile is workflow.RecoverReconcile's response. Cleared is
// true when an in_progress claim was actually removed; PriorHolder
// identifies who was holding it.
type RecoverReconcile struct {
	Ref         refs.Series   `json:"ref"`
	Cleared     bool          `json:"cleared"`
	PriorHolder *coord.Holder `json:"priorHolder,omitempty"`
}

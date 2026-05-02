// Package reconcile owns the planning + applying + recovering of
// kura's reconcile workflow. Planning consumes a series's persisted
// state and produces a fully-unrolled step plan (file moves + dir
// removes); applying executes the steps in order, logs each attempt,
// and writes terminal state back to series.json.
//
// Persistence of the plan + apply log lives in
// internal/storage/planfile (one .jsonl file per token under
// .kura/reconcile/<token>.jsonl).
//
// The reconcile package does not import the workflow package; the
// workflow side wraps reconcile in thin shims that translate to the
// shared workflow.Deps shape.
package reconcile

import (
	"context"
	"log/slog"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
)

// Deps is the cross-call dependency set the reconcile package needs.
// Built once by the workflow shim and passed by value into the package
// entry points (Plan, Apply, Recover).
type Deps struct {
	// LibRoot is the absolute path of the kura library root.
	LibRoot string

	// InboxRoot is the absolute path of the inbox root used by stage
	// flow. Reconcile steps that move files out of inbox carry the
	// inbox: selector form in step.From and resolve via this root at
	// apply time.
	InboxRoot string

	// Now returns the current time. Tests inject a fixed clock.
	Now func() time.Time

	// Coordinator serializes mutations against the same series within
	// the same process. CLI uses the no-op variant; long-running
	// processes use the real one.
	Coordinator coord.Coordinator

	// Logger is the structured logger reconcile writes audit events
	// (file moves, claim acquire/release) to. Optional — nil disables
	// audit logging.
	Logger *slog.Logger

	// Index is the in-memory metadata-ref → series-ref cache.
	Index *indexfile.Index

	// Jobs is the registry that backs the async Apply job. Required
	// for Apply; unused by Plan / Recover.
	Jobs *jobs.Registry

	// UpdateIndex is the workflow-side callback that updates the
	// indexfile row for the given series after a successful
	// SaveCAS. Reconcile calls back into workflow because the index
	// CAS dance lives there. Required for Apply.
	UpdateIndex func(ctx context.Context, model *series.Series, op string) error
}

// log returns deps.Logger when non-nil, or a discarding logger
// otherwise. Guarantees call sites can write `deps.log().Info(...)`
// without nil checks.
func (d Deps) log() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.New(slog.DiscardHandler)
}

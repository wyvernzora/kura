package series

import (
	"context"

	reconcileworkflow "github.com/wyvernzora/kura/internal/series/reconcile"
)

type ReconcilePlan = reconcileworkflow.ReconcilePlan

type FileMove = reconcileworkflow.FileMove

type Change = reconcileworkflow.Change

type ChangeKind = reconcileworkflow.ChangeKind

const (
	ChangeAdd     = reconcileworkflow.ChangeAdd
	ChangeMove    = reconcileworkflow.ChangeMove
	ChangeReplace = reconcileworkflow.ChangeReplace
)

type Replaced = reconcileworkflow.Replaced

type ReconcileResult = reconcileworkflow.ReconcileResult

type PlanStaleError = reconcileworkflow.PlanStaleError

type StoredReconcilePlan = reconcileworkflow.StoredReconcilePlan

type ReconcilePlanExpiredError = reconcileworkflow.ReconcilePlanExpiredError

type ReconcilePlanAlreadyAppliedError = reconcileworkflow.ReconcilePlanAlreadyAppliedError

func (h Handle) PlanReconcile() (ReconcilePlan, error) {
	return reconcileworkflow.NewRunner(h.root(), h.ref, h.now).PlanReconcile()
}

func (h Handle) CreateReconcilePlan() (StoredReconcilePlan, error) {
	return reconcileworkflow.NewRunner(h.root(), h.ref, h.now).CreateReconcilePlan()
}

func (h Handle) ApplyReconcileToken(ctx context.Context, token string) (ReconcileResult, error) {
	return reconcileworkflow.NewRunner(h.root(), h.ref, h.now).ApplyReconcileToken(ctx, token)
}

func (h Handle) ApplyReconcile(ctx context.Context, plan ReconcilePlan) (ReconcileResult, error) {
	return reconcileworkflow.NewRunner(h.root(), h.ref, h.now).ApplyReconcile(ctx, plan)
}

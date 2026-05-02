package reconcile

import (
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/storage/planfile"
)

const reconcilePlanTTL = 5 * time.Minute

type StoredReconcilePlan struct {
	Token     string        `json:"token"`
	CreatedAt time.Time     `json:"createdAt"`
	ExpiresAt time.Time     `json:"expiresAt"`
	Plan      ReconcilePlan `json:"plan"`
}

type ReconcilePlanExpiredError struct {
	Token     string
	ExpiresAt time.Time
}

func (err ReconcilePlanExpiredError) Error() string {
	return fmt.Sprintf("series: reconcile plan %s expired at %s", err.Token, err.ExpiresAt.Format(time.RFC3339))
}

type ReconcilePlanAlreadyAppliedError struct {
	Token string
}

func (err ReconcilePlanAlreadyAppliedError) Error() string {
	return fmt.Sprintf("series: reconcile plan %s was already applied", err.Token)
}

func (h Runner) CreateReconcilePlan() (StoredReconcilePlan, error) {
	plan, metadataRef, err := h.planReconcile()
	if err != nil {
		return StoredReconcilePlan{}, err
	}
	if !plan.HasChanges() {
		return StoredReconcilePlan{Plan: plan}, nil
	}
	token := ulid.Make().String()
	createdAt := h.now().UTC()
	stored := StoredReconcilePlan{
		Token:     token,
		CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(reconcilePlanTTL),
		Plan:      plan,
	}
	record := planfile.PlanRecord{
		Token:       token,
		CreatedAt:   stored.CreatedAt,
		ExpiresAt:   stored.ExpiresAt,
		Series:      plan.Series,
		MetadataRef: metadataRef,
		FileTitle:   plan.FileTitle,
		Snapshot:    plan.Snapshot,
		Changes:     toPlanFileChanges(plan.Changes),
	}
	if err := planfile.WritePlan(h.root(), h.ref, record); err != nil {
		return StoredReconcilePlan{}, err
	}
	return stored, nil
}

func (h Runner) loadStoredReconcilePlan(token string) (StoredReconcilePlan, bool, error) {
	record, applied, err := planfile.ReadPlan(h.root(), h.ref, token)
	if err != nil {
		return StoredReconcilePlan{}, false, err
	}
	if record.Series != h.ref {
		return StoredReconcilePlan{}, false, PlanStaleError{Series: record.Series}
	}
	currentSeries, err := h.load()
	if err != nil {
		return StoredReconcilePlan{}, false, err
	}
	if record.MetadataRef != currentSeries.Metadata {
		return StoredReconcilePlan{}, false, PlanStaleError{Series: h.ref}
	}
	return StoredReconcilePlan{
		Token:     record.Token,
		CreatedAt: record.CreatedAt,
		ExpiresAt: record.ExpiresAt,
		Plan:      fromPlanFileRecord(record),
	}, applied, nil
}

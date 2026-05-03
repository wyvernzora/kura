package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
)

// Snapshot returns the hex sha256 of the input bytes. Workflows compute this
// over the raw series.json bytes at plan time; apply re-computes and compares
// to detect drift.
func Snapshot(seriesBytes []byte) string {
	sum := sha256.Sum256(seriesBytes)
	return hex.EncodeToString(sum[:])
}

// ValidateSnapshot returns StaleSnapshotError if the current bytes do not
// hash to the snapshot recorded in the plan.
func ValidateSnapshot(plan Plan, currentBytes []byte) error {
	if Snapshot(currentBytes) != plan.Snapshot {
		return StaleSnapshotError{Series: plan.Series}
	}
	return nil
}

// StaleSnapshotError signals that the on-disk series state has changed since
// the plan was created. Callers re-plan and re-apply.
type StaleSnapshotError struct {
	Series refs.Series
}

func (e StaleSnapshotError) Error() string {
	return fmt.Sprintf("reconcile: snapshot for %s is stale", e.Series)
}

func (e StaleSnapshotError) Kind() string     { return errkind.KindStaleSnapshot }
func (e StaleSnapshotError) Category() string { return errkind.CategoryInternalError }
func (e StaleSnapshotError) Data() map[string]any {
	return map[string]any{"series": e.Series.String()}
}

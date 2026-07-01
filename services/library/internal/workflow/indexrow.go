package workflow

import (
	"context"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/series"
)

// updateIndexRow rebuilds the row for model from its current state and
// upserts it under the index CAS lock. Used after a successful series.json
// SaveCAS to keep the materialized JSONL view fresh.
//
// Series mutations that touch fields the row surfaces (counts, status,
// quality, staged flag, lastScanned) must call this. Mutations that only
// touch in_progress (claim acquire / release) skip it — the row doesn't
// reflect that state, and the extra CAS write would just contend.
func updateIndexRow(ctx context.Context, deps Deps, model *series.Series, op string) error {
	if deps.Index == nil {
		return nil
	}
	return deps.Index.SaveModel(ctx, model, coord.NewMutator(op))
}

package workflow

import (
	"context"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/series"
)

// updateIndexModel writes model into the index source snapshot. Used after a
// successful series.json SaveCAS so selector lookup and list projections see
// the same source data as the series file.
//
// Series mutations that touch fields the row surfaces (counts, status,
// quality, staged flag, lastScanned) must call this. Mutations that only
// touch in_progress (claim acquire / release) skip it — the row doesn't
// reflect that state, and the extra index write would just contend.
func updateIndexModel(ctx context.Context, deps Deps, model *series.Series, op string) error {
	if deps.Index == nil {
		return nil
	}
	return deps.Index.SaveModel(ctx, model, coord.NewMutator(op))
}

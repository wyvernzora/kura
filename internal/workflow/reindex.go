package workflow

import (
	"context"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
)

// Reindex walks the library root, materializes a row per series from
// per-series series.json, and rewrites index.jsonl via hash CAS.
// Conflict (peer mutated mid-walk) triggers retry per coord policy.
//
// Local-only: never invokes the metadata provider.
func Reindex(ctx context.Context, deps Deps) error {
	return deps.Coordinator.WithIndex(ctx, func() error {
		return coord.RetryOnConflict(coord.AttemptsFromEnv(), func() error {
			rebuilt, err := indexfile.Rebuild(ctx, deps.LibRoot, indexfile.BuildRow)
			if err != nil {
				return err
			}
			rows := rebuilt.Rows()

			// Read current disk hash (or treat absent file as create-path).
			expected := ""
			current, loadErr := indexfile.LoadCAS(deps.LibRoot)
			if loadErr == nil {
				expected = current.Hash
			}
			if deps.Index != nil {
				return deps.Index.SaveAndAdopt(expected, rows, coord.NewMutator("reindex"))
			}
			return indexfile.SaveCAS(deps.LibRoot, expected, rows, coord.NewMutator("reindex"))
		})
	})
}

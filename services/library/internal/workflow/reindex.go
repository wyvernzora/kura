package workflow

import (
	"context"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
)

// Reindex walks the library root, materializes a row per series from
// per-series series.json, and rewrites index.jsonl via hash CAS.
// Conflict (peer mutated mid-walk) triggers retry per coord policy.
//
// Job-shaped: returns *jobs.Job[response.ReindexResult] so callers
// can stream progress events via the registry. CLI consumes them via
// SSE on /jobs/{id}/stream and renders a live status line.
//
// Local-only: never invokes the metadata provider.
func Reindex(ctx context.Context, deps Deps) *jobs.Job[response.ReindexResult] {
	return jobs.Submit(deps.Jobs, ctx, jobs.KindReindex, refs.Series{}, func(jobCtx context.Context) (response.ReindexResult, error) {
		var result response.ReindexResult
		err := deps.Coordinator.WithIndex(jobCtx, func() error {
			return coord.RetryOnConflict(coord.AttemptsFromEnv(), func() error {
				rebuilt, err := indexfile.Rebuild(jobCtx, deps.LibRoot, indexfile.BuildRow)
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
					if err := deps.Index.SaveAndAdopt(expected, rows, coord.NewMutator("reindex")); err != nil {
						return err
					}
				} else {
					if err := indexfile.SaveCAS(deps.LibRoot, expected, rows, coord.NewMutator("reindex")); err != nil {
						return err
					}
				}
				result.Rows = len(rows)
				return nil
			})
		})
		if err != nil {
			return response.ReindexResult{}, err
		}
		return result, nil
	})
}

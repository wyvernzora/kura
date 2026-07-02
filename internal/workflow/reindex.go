package workflow

import (
	"context"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/response"
)

// Reindex walks the library root, reloads every series.json into the
// in-memory index, and rewrites the index.jsonl source snapshot. The
// rebuild holds the index guard for the whole walk, so concurrent
// mutators queue behind it instead of interleaving.
//
// Job-shaped: returns *jobs.Job[response.ReindexResult] so callers
// can stream progress events via the registry. CLI consumes them via
// SSE on /jobs/{id}/stream and renders a live status line.
//
// Local-only: never invokes the metadata provider.
func Reindex(ctx context.Context, deps Deps) *jobs.Job[response.ReindexResult] {
	return jobs.Submit(deps.Jobs, ctx, jobs.KindReindex, refs.Series{}, func(jobCtx context.Context) (response.ReindexResult, error) {
		if err := deps.Index.RebuildNow(jobCtx, "reindex"); err != nil {
			return response.ReindexResult{}, err
		}
		return response.ReindexResult{Rows: deps.Index.Len()}, nil
	})
}

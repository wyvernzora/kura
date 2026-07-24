package workflow

import (
	"context"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

// Reindex walks the library root, reloads every series.json into the
// in-memory index, and rewrites the index.jsonl source snapshot. The
// rebuild holds the index guard for the whole walk, so concurrent
// mutators queue behind it instead of interleaving.
//
// Job-shaped: returns *jobs.Job[api.ReindexResult] so callers
// can stream progress events via the registry. CLI consumes them via
// SSE on /jobs/{id}/stream and renders a live status line.
//
// Local-only: never invokes the metadata provider.
func Reindex(ctx context.Context, deps Deps) *jobs.Job[api.ReindexResult] {
	return jobs.Submit(deps.Jobs, ctx, jobs.KindReindex, refs.Series{}, func(jobCtx context.Context) (api.ReindexResult, error) {
		if err := deps.Index.RebuildNow(jobCtx, "reindex"); err != nil {
			return api.ReindexResult{}, err
		}
		return api.ReindexResult{Rows: deps.Index.Len()}, nil
	})
}

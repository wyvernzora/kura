package workflow

import (
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
)

// updateIndexRow rebuilds the row for model from its current state and
// upserts it under the index CAS lock. Used after a successful series.json
// SaveCAS to keep the materialized JSONL view fresh.
//
// Series mutations that touch fields the row surfaces (counts, status,
// quality, staged flag, lastScanned) must call this. Mutations that only
// touch in_progress (claim acquire / release) skip it — the row doesn't
// reflect that state, and the extra CAS write would just contend.
func updateIndexRow(deps Deps, model *series.Series, op string) error {
	row := indexfile.BuildRowFromModel(model, deps.Now())
	return withIndexCAS(deps, op, func(loaded indexfile.Loaded) ([]indexfile.Row, error) {
		return appendOrReplaceRow(loaded.Rows, row), nil
	})
}

// appendOrReplaceRow inserts row, replacing any existing row keyed by
// the same series ref. The slice is rewritten; callers downstream of
// SaveCAS rely on the return value, not in-place mutation.
func appendOrReplaceRow(rows []indexfile.Row, row indexfile.Row) []indexfile.Row {
	for i := range rows {
		if rows[i].Series == row.Series {
			rows[i] = row
			return rows
		}
	}
	return append(rows, row)
}

package workflow

import (
	"context"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// Reindex walks the library root, gathers metadata refs from each
// per-series series.json, and rewrites index.tsv via hash CAS. Conflict
// (peer mutated mid-walk) triggers retry per coord policy.
//
// Local-only: never invokes the metadata provider.
func Reindex(ctx context.Context, deps Deps) error {
	return deps.Coordinator.WithIndexRetry(func() error {
		rebuilt, err := indexfile.Rebuild(ctx, deps.LibRoot, func(_ context.Context, ref refs.Series) (refs.Metadata, error) {
			return seriesfile.ReadMetadataRef(deps.LibRoot, ref)
		})
		if err != nil {
			return err
		}
		entries := rebuilt.Entries()

		// Read current disk hash (or treat absent file as create-path).
		expected := ""
		current, loadErr := indexfile.LoadCAS(deps.LibRoot)
		if loadErr == nil {
			expected = current.Hash
		}
		if err := indexfile.SaveCAS(deps.LibRoot, expected, entries, coord.NewMutator("reindex")); err != nil {
			return err
		}
		if deps.Index != nil {
			deps.Index.ReplaceEntries(entries)
		}
		return nil
	})
}

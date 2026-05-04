package workflow

import (
	"errors"
	"os"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
)

// withIndexCAS runs the standard load-mutate-save-CAS sequence under
// the coordinator. mutate receives the freshly loaded rows and returns
// the new row slice. Conflicts trigger retry per coord policy; second
// conflict surfaces.
//
// On success the in-memory deps.Index is replaced with the new rows so
// subsequent reads through deps.Index reflect the write.
func withIndexCAS(deps Deps, op string, mutate func(loaded indexfile.Loaded) ([]indexfile.Row, error)) error {
	return deps.Coordinator.WithIndexRetry(func() error {
		loaded, err := loadIndexEntries(deps)
		if err != nil {
			return err
		}
		rows, err := mutate(loaded)
		if err != nil {
			return err
		}
		if err := indexfile.SaveCAS(deps.LibRoot, loaded.Hash, rows, coord.NewMutator(op)); err != nil {
			return err
		}
		if deps.Index != nil {
			deps.Index.ReplaceRows(rows)
		}
		return nil
	})
}

// loadIndexEntries reads the current index from disk. Treats absence as
// an empty load with empty Hash so SaveCAS uses the create path.
func loadIndexEntries(deps Deps) (indexfile.Loaded, error) {
	loaded, err := indexfile.LoadCAS(deps.LibRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return indexfile.Loaded{}, nil
		}
		return indexfile.Loaded{}, err
	}
	return loaded, nil
}

package reconcile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wyvernzora/kura/internal/domain/selector"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

// planExtras builds the ordered step set for every stagedExtras entry.
// Each entry's source path is walked at plan time:
//
//   - Plain file source → one file_move step into Season N/Extra/[Prefix]/<basename>.
//   - Directory source → walk the tree; each contained file becomes a
//     file_move step. Subdirectories that the walk drains become
//     dir_remove steps (deepest-first), with the top-level source
//     directory removed last.
//
// Output per stagedExtras entry order: file_moves (deepest first),
// then dir_remove steps (deepest first), then top-level dir_remove.
// Across stagedExtras entries: sorted by ULID for determinism.
//
// Plan-time walk captures the file list at the moment of planning;
// apply moves only what was captured. Files added to the source after
// plan are silently ignored. Apply re-stat per step naturally surfaces
// "file vanished" as a per-step error.
func planExtras(token string, model *series.Series, inboxRoot string) ([]Step, error) {
	if len(model.StagedExtras) == 0 {
		return nil, nil
	}

	items := make([]series.StagedExtraItem, len(model.StagedExtras))
	copy(items, model.StagedExtras)
	sort.Slice(items, func(i, j int) bool { return items[i].ID.String() < items[j].ID.String() })

	out := make([]Step, 0)
	for _, item := range items {
		steps, err := planOneExtra(token, item, inboxRoot)
		if err != nil {
			return nil, err
		}
		out = append(out, steps...)
	}
	return out, nil
}

// resolveExtraSource turns the persisted Path field (an inbox: selector)
// into an absolute filesystem path. Only inbox: selectors are accepted;
// bare absolute paths are rejected to prevent out-of-policy source access.
func resolveExtraSource(path, inboxRoot string) (string, string, error) {
	if !strings.HasPrefix(path, string(selector.Inbox)+":") {
		return "", path, fmt.Errorf("reconcile: extras source %q is not an inbox: selector; reset and re-stage this entry", path)
	}
	if inboxRoot == "" {
		return "", path, fmt.Errorf("reconcile: inbox root not configured but extras source uses inbox: selector %q", path)
	}
	sel, err := selector.ParseInbox(path)
	if err != nil {
		return "", path, fmt.Errorf("reconcile: extras source %q: %w", path, err)
	}
	return sel.Resolve(inboxRoot), path, nil
}

func planOneExtra(token string, item series.StagedExtraItem, inboxRoot string) ([]Step, error) {
	owner := Owner{
		Kind:    OwnerExtra,
		ExtraID: item.ID.String(),
		Season:  item.Season,
		Prefix:  item.Prefix,
	}

	absSource, fromForStep, err := resolveExtraSource(item.Path, inboxRoot)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(absSource)
	if err != nil {
		return nil, fmt.Errorf("reconcile: extras source %q: %w", item.Path, err)
	}

	if !info.IsDir() {
		basename := filepath.Base(absSource)
		to := paths.ExtraRel(item.Season, item.Prefix, basename)
		return []Step{makeFileMove(token, owner, fromForStep, to)}, nil
	}

	rootBase := filepath.Base(absSource)
	destRoot := paths.ExtraRel(item.Season, item.Prefix, rootBase)

	// Walk the tree; collect file paths and dir paths (excluding the
	// root). Both lists end up sorted by depth descending (deepest
	// first) for the dir_remove ordering.
	type entry struct {
		rel   string // path relative to absSource
		isDir bool
	}
	entries := make([]entry, 0)
	walkErr := filepath.WalkDir(absSource, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absSource, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		entries = append(entries, entry{rel: rel, isDir: d.IsDir()})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("reconcile: walk extras source %q: %w", item.Path, walkErr)
	}

	// File moves first (deepest first), then dir_removes (deepest
	// first), then root dir_remove.
	files := make([]entry, 0)
	dirs := make([]entry, 0)
	for _, e := range entries {
		if e.isDir {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	sort.Slice(files, func(i, j int) bool { return depth(files[i].rel) > depth(files[j].rel) })
	sort.Slice(dirs, func(i, j int) bool { return depth(dirs[i].rel) > depth(dirs[j].rel) })

	out := make([]Step, 0, len(files)+len(dirs)+1)
	for _, f := range files {
		from := filepath.Join(absSource, filepath.FromSlash(f.rel))
		to := destRoot + "/" + filepath.ToSlash(f.rel)
		out = append(out, makeFileMove(token, owner, from, to))
	}
	for _, d := range dirs {
		// dir_remove path is the *source* directory (we delete the
		// source after copying its contents). Apply absolutizes via
		// the sourcing root.
		out = append(out, makeDirRemove(token, owner, filepath.Join(absSource, filepath.FromSlash(d.rel))))
	}
	// Top-level source dir removal — keep selector form so the step
	// log shows "inbox:..." for inbox-sourced extras.
	out = append(out, makeDirRemove(token, owner, fromForStep))
	return out, nil
}

func depth(rel string) int {
	if rel == "" || rel == "." {
		return 0
	}
	d := 1
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' || rel[i] == filepath.Separator {
			d++
		}
	}
	return d
}

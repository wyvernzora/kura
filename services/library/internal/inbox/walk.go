// Package inbox owns the inbox-side dir walker plus hidden-file rules.
// Selector primitives live in internal/domain/selector — this package
// composes them with filesystem walking.
package inbox

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/domain/selector"
	"github.com/wyvernzora/kura/internal/errkind"
	"golang.org/x/text/unicode/norm"
)

// Kind labels each walked entry.
type Kind string

const (
	KindFile    Kind = "file"
	KindDir     Kind = "dir"
	KindSymlink Kind = "symlink"
)

// Entry is one item returned by Walk. RelPath is forward-slash, NFC,
// relative to the inbox root (not to the start path of the walk). Size
// is meaningful for files only; dirs and symlinks carry 0.
type Entry struct {
	Name          string
	RelPath       string
	Kind          Kind
	Size          int64
	MTime         time.Time
	SymlinkTarget string
}

// Selector returns the inbox: selector form of the entry's path.
func (e Entry) Selector() selector.Path {
	if e.RelPath == "" {
		return selector.Path{}
	}
	return selector.Path{Scheme: selector.Inbox, Relative: e.RelPath}
}

// Options configures Walk.
type Options struct {
	// Path is a slash-form relative path under the inbox root. Empty
	// means the inbox root itself. Validated via CleanRelativePath.
	Path string

	// Recursive controls multi-level traversal. When true, Depth caps
	// the levels visited; depth=1 returns immediate children only,
	// matching !Recursive.
	Recursive bool
	Depth     int

	// Limit caps the number of entries returned across all levels.
	// When the discovered set exceeds Limit, Result.Truncated is set
	// and the first Limit entries (after sorting) are kept.
	Limit int

	// Kind, when non-empty, restricts to entries of that kind.
	// Accepted: "file", "dir", "symlink".
	Kind string

	// NameGlob is a filepath.Match-compatible pattern matched against
	// each entry's basename. Empty means no filter.
	NameGlob string

	// IncludeHidden disables the default hidden-file skip.
	IncludeHidden bool
}

// Result is the output of Walk.
type Result struct {
	Path        string
	Entries     []Entry
	Truncated   bool
	ElidedCount int
}

// PathNotFoundError signals the requested subpath does not exist under
// the inbox root.
type PathNotFoundError struct {
	Path string
}

func (e *PathNotFoundError) Error() string {
	return fmt.Sprintf("inbox: path %q does not exist", e.Path)
}

func (e *PathNotFoundError) Kind() string     { return errkind.KindNotFound }
func (e *PathNotFoundError) Category() string { return errkind.CategoryInvalidParams }
func (e *PathNotFoundError) Data() map[string]any {
	return map[string]any{"path": e.Path}
}

// PathNotDirError signals the requested subpath exists but is not a
// directory.
type PathNotDirError struct {
	Path string
}

func (e *PathNotDirError) Error() string {
	return fmt.Sprintf("inbox: path %q is not a directory", e.Path)
}

func (e *PathNotDirError) Kind() string     { return errkind.KindInvalidRef }
func (e *PathNotDirError) Category() string { return errkind.CategoryInvalidParams }
func (e *PathNotDirError) Data() map[string]any {
	return map[string]any{"path": e.Path}
}

// InvalidGlobError signals NameGlob has malformed syntax.
type InvalidGlobError struct {
	Pattern string
	Err     error
}

func (e *InvalidGlobError) Error() string {
	return fmt.Sprintf("inbox: invalid name glob %q: %v", e.Pattern, e.Err)
}

func (e *InvalidGlobError) Unwrap() error { return e.Err }

func (e *InvalidGlobError) Kind() string     { return errkind.KindInvalidRef }
func (e *InvalidGlobError) Category() string { return errkind.CategoryInvalidParams }
func (e *InvalidGlobError) Data() map[string]any {
	return map[string]any{"pattern": e.Pattern}
}

// Walk enumerates entries under inboxRoot/<opts.Path>. The walk is
// non-following for symlinks (they're surfaced as Kind=symlink with
// SymlinkTarget set, not recursed into). Names from the filesystem are
// NFC-normalized in returned entries; recursion uses the on-disk name
// for stat'ing so NFD-storage filesystems (macOS HFS+/APFS) still work.
func Walk(inboxRoot string, opts Options) (Result, error) {
	if inboxRoot == "" {
		return Result{}, errors.New("inbox: inboxRoot is empty")
	}
	relPath, err := selector.CleanRelative(opts.Path)
	if err != nil {
		return Result{}, err
	}
	if opts.NameGlob != "" {
		if _, mErr := filepath.Match(opts.NameGlob, "probe"); mErr != nil {
			return Result{}, &InvalidGlobError{Pattern: opts.NameGlob, Err: mErr}
		}
	}
	if opts.Kind != "" && opts.Kind != string(KindFile) && opts.Kind != string(KindDir) && opts.Kind != string(KindSymlink) {
		return Result{}, fmt.Errorf("inbox: invalid kind filter %q", opts.Kind)
	}
	if opts.Limit <= 0 {
		return Result{}, fmt.Errorf("inbox: limit must be positive (got %d)", opts.Limit)
	}

	depth := opts.Depth
	if !opts.Recursive {
		depth = 1
	} else if depth <= 0 {
		depth = 1
	}

	base := inboxRoot
	if relPath != "" {
		base = filepath.Join(inboxRoot, filepath.FromSlash(relPath))
	}

	if err := guardWithinRoot(inboxRoot, base); err != nil {
		return Result{}, err
	}

	info, err := os.Lstat(base)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Result{}, &PathNotFoundError{Path: relPath}
		}
		return Result{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Walk's start path can't itself be a symlink (would conflict
		// with the no-follow contract for traversal).
		return Result{}, &selector.PathOutsideRootError{Path: relPath}
	}
	if !info.IsDir() {
		return Result{}, &PathNotDirError{Path: relPath}
	}

	all, err := walkDir(base, relPath, depth, opts)
	if err != nil {
		return Result{}, err
	}

	sortEntries(all)

	res := Result{Path: relPath, Entries: all}
	if len(all) > opts.Limit {
		res.ElidedCount = len(all) - opts.Limit
		res.Truncated = true
		res.Entries = all[:opts.Limit]
	}
	return res, nil
}

// walkDir reads one directory and (if depthRemaining > 1) recurses
// into subdirectories. relBase is the slash-form path from the inbox
// root to dirPath; entries' RelPath is built by joining relBase + name.
func walkDir(dirPath, relBase string, depthRemaining int, opts Options) ([]Entry, error) {
	dirEntries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, de := range dirEntries {
		diskName := de.Name()
		normName := norm.NFC.String(diskName)
		if !opts.IncludeHidden && isHidden(normName) {
			continue
		}
		relPath := normName
		if relBase != "" {
			relPath = relBase + "/" + normName
		}
		absPath := filepath.Join(dirPath, diskName)
		info, err := os.Lstat(absPath)
		if err != nil {
			// Race: file vanished between ReadDir and Lstat. Skip.
			continue
		}
		entry := Entry{
			Name:    normName,
			RelPath: relPath,
			MTime:   info.ModTime(),
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			entry.Kind = KindSymlink
			if target, terr := os.Readlink(absPath); terr == nil {
				entry.SymlinkTarget = norm.NFC.String(target)
			}
		case info.IsDir():
			entry.Kind = KindDir
		default:
			entry.Kind = KindFile
			entry.Size = info.Size()
		}
		if matchesFilters(entry, opts) {
			out = append(out, entry)
		}
		// Recurse only into real directories (no symlink follow), and
		// only when depth budget remains.
		if entry.Kind == KindDir && depthRemaining > 1 {
			child, cErr := walkDir(absPath, relPath, depthRemaining-1, opts)
			if cErr != nil {
				return nil, cErr
			}
			out = append(out, child...)
		}
	}
	return out, nil
}

// matchesFilters reports whether an entry passes the kind / name-glob
// filters in opts.
func matchesFilters(e Entry, opts Options) bool {
	if opts.Kind != "" && string(e.Kind) != opts.Kind {
		return false
	}
	if opts.NameGlob != "" {
		matched, err := filepath.Match(opts.NameGlob, e.Name)
		if err != nil || !matched {
			return false
		}
	}
	return true
}

// sortEntries sorts in place: mtime desc, then name asc tiebreak. A
// stable sort isn't required because the (mtime, name) key is unique
// in practice — name asc fully orders ties.
func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if !a.MTime.Equal(b.MTime) {
			return a.MTime.After(b.MTime)
		}
		return a.Name < b.Name
	})
}

// guardWithinRoot verifies target is at or below inboxRoot. Symlink
// resolution is applied to both paths when the target exists; when it
// doesn't, both paths fall back to cleaned-absolute (avoids macOS's
// /var → /private/var asymmetry breaking the prefix check on
// not-yet-existing paths).
func guardWithinRoot(inboxRoot, target string) error {
	rootAbs, err := filepath.Abs(inboxRoot)
	if err != nil {
		return fmt.Errorf("inbox: resolve root: %w", err)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("inbox: resolve target: %w", err)
	}

	rootResolved, rootErr := filepath.EvalSymlinks(rootAbs)
	targetResolved, targetErr := filepath.EvalSymlinks(targetAbs)
	if rootErr != nil || targetErr != nil {
		// Either path has a non-existent component. Compare cleaned-
		// absolute (consistent symlink treatment for both).
		rootResolved = filepath.Clean(rootAbs)
		targetResolved = filepath.Clean(targetAbs)
	}

	rel, err := filepath.Rel(rootResolved, targetResolved)
	if err != nil {
		return fmt.Errorf("inbox: resolve relative: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return &selector.PathOutsideRootError{Path: target}
	}
	return nil
}

package workflow

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wyvernzora/kura/internal/domain/selector"
)

// seriesSelector returns path expressed as a `series:<rel>` selector
// scoped to seriesRoot. Absolute paths must live inside seriesRoot;
// out-of-root paths panic, because the response contract promises
// scheme-tagged paths and an outside-root call here means the
// workflow handed the wrong path to the wrong helper. Already-
// relative paths are accepted as-is (taken to be series-relative
// slash form by convention). Empty input returns empty.
func seriesSelector(seriesRoot, path string) string {
	return makeSelector(selector.Series, seriesRoot, path)
}

// inboxSelector returns path expressed as an `inbox:<rel>` selector
// scoped to inboxRoot. Same semantics as seriesSelector.
func inboxSelector(inboxRoot, path string) string {
	return makeSelector(selector.Inbox, inboxRoot, path)
}

// librarySelector returns path expressed as a `library:<rel>` selector
// scoped to libRoot. Same semantics as seriesSelector.
func librarySelector(libRoot, path string) string {
	return makeSelector(selector.Library, libRoot, path)
}

func makeSelector(scheme selector.Scheme, root, path string) string {
	if path == "" {
		return ""
	}
	// Already a known scheme-tagged selector — pass through. Stage
	// records persist `inbox:<rel>` / `series:<rel>` directly, so a
	// staged-record path can arrive here pre-tagged regardless of which
	// scheme the caller intended.
	if existing, err := selector.Parse(path); err == nil {
		return existing.String()
	}
	rel := path
	if filepath.IsAbs(path) {
		if root == "" {
			panic(fmt.Sprintf("workflow: %s selector requires non-empty root for absolute %q", scheme, path))
		}
		computed, err := filepath.Rel(root, path)
		if err != nil {
			panic(fmt.Sprintf("workflow: path %q outside %s root %q: %v", path, scheme, root, err))
		}
		rel = computed
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		panic(fmt.Sprintf("workflow: path %q escapes %s root %q (rel=%q)", path, scheme, root, rel))
	}
	return (selector.Path{Scheme: scheme, Relative: rel}).String()
}

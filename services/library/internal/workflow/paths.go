package workflow

import (
	"path/filepath"
	"strings"
)

// relativeToSeries returns path expressed relative to seriesRoot in
// slash form when path lives inside it. Paths outside the series root
// (e.g. inbox-staged files) are returned unchanged so callers can still
// reach them. Empty input returns empty.
func relativeToSeries(seriesRoot, path string) string {
	if path == "" || seriesRoot == "" {
		return path
	}
	rel, err := filepath.Rel(seriesRoot, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return path
	}
	return filepath.ToSlash(rel)
}

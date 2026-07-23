// Package seriesdir owns the validated filesystem-directory value type
// for a single tracked series. Pure path arithmetic lives in
// internal/storage/paths; this package adds the os.Stat-backed validation
// that an in-memory SeriesDir actually points at a directory on disk.
package seriesdir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SeriesDir is a validated filesystem directory path for one series.
type SeriesDir struct {
	path string
}

// Parse stat-checks path and returns a SeriesDir wrapping the cleaned
// absolute form. Returns an error if path is empty, missing, or not a
// directory.
func Parse(path string) (SeriesDir, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return SeriesDir{}, errors.New("seriesdir: path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return SeriesDir{}, err
	}
	if !info.IsDir() {
		return SeriesDir{}, fmt.Errorf("seriesdir: %q is not a directory", path)
	}
	return SeriesDir{path: filepath.Clean(path)}, nil
}

// Path returns the cleaned absolute filesystem path.
func (d SeriesDir) Path() string {
	return d.path
}

// String mirrors Path for fmt-style use.
func (d SeriesDir) String() string {
	return d.path
}

// Name returns the basename of the series directory.
func (d SeriesDir) Name() string {
	return filepath.Base(d.path)
}

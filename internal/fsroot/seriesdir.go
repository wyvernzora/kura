package fsroot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SeriesDir is a validated filesystem directory for one series.
type SeriesDir struct {
	path string
}

func ParseSeriesDir(path string) (SeriesDir, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return SeriesDir{}, errors.New("library: series path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return SeriesDir{}, err
	}
	if !info.IsDir() {
		return SeriesDir{}, fmt.Errorf("library: series path %q is not a directory", path)
	}
	return SeriesDir{path: filepath.Clean(path)}, nil
}

func (d SeriesDir) Path() string {
	return d.path
}

func (d SeriesDir) String() string {
	return d.path
}

func (d SeriesDir) Name() string {
	return filepath.Base(d.path)
}

func (d SeriesDir) MetadataPath() string {
	return SeriesMetadataPath(d.path)
}

func (d SeriesDir) CleanRelPath(path string) (string, error) {
	return CleanSeriesRelPath(path)
}

func (d SeriesDir) JoinRel(path string) (string, error) {
	relPath, err := d.CleanRelPath(path)
	if err != nil {
		return "", err
	}
	return filepath.Join(d.path, filepath.FromSlash(relPath)), nil
}

func (d SeriesDir) Contains(path string) bool {
	return containsPath(d.path, path)
}

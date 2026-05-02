package series

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wyvernzora/kura/internal/series/wire"
)

// SeriesDir is a filesystem directory for one series.
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

func (d SeriesDir) JoinRel(path string) (string, error) {
	relPath, err := cleanSeriesRelPath(path)
	if err != nil {
		return "", err
	}
	return filepath.Join(d.path, filepath.FromSlash(relPath)), nil
}

func cleanSeriesRelPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path is required")
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("path %q must be relative to the series root", path)
	}

	slashPath := filepath.ToSlash(path)
	clean := filepath.Clean(filepath.FromSlash(slashPath))
	if clean == "." || clean == string(filepath.Separator) {
		return "", fmt.Errorf("path %q must point to a file", path)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the series root", path)
	}
	clean = filepath.ToSlash(clean)
	if slices.Contains(strings.Split(clean, "/"), wire.KuraDir) {
		return "", fmt.Errorf("path %q cannot point inside .kura", path)
	}
	return clean, nil
}

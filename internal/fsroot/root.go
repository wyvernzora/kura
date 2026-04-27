package fsroot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LibraryRoot is a validated absolute anime library root.
type LibraryRoot struct {
	path string
}

func ParseLibraryRoot(path string) (LibraryRoot, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return LibraryRoot{}, errors.New("library: root path is required")
	}
	if !filepath.IsAbs(path) {
		return LibraryRoot{}, errors.New("library: root path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return LibraryRoot{}, err
	}
	if !info.IsDir() {
		return LibraryRoot{}, fmt.Errorf("library: root path %q is not a directory", path)
	}
	return LibraryRoot{path: filepath.Clean(path)}, nil
}

func (r LibraryRoot) Path() string {
	return r.path
}

func (r LibraryRoot) String() string {
	return r.path
}

func (r LibraryRoot) Join(parts ...string) string {
	native := make([]string, 0, len(parts)+1)
	native = append(native, r.path)
	for _, part := range parts {
		native = append(native, filepath.FromSlash(part))
	}
	return filepath.Join(native...)
}

func (r LibraryRoot) Contains(path string) bool {
	return containsPath(r.path, path)
}

func (r LibraryRoot) SeriesDir(name string) (SeriesDir, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return SeriesDir{}, errors.New("library: series directory name is required")
	}
	if filepath.IsAbs(name) || name != filepath.Base(name) {
		return SeriesDir{}, fmt.Errorf("library: series directory %q must be a direct child of the library root", name)
	}
	return ParseSeriesDir(filepath.Join(r.path, name))
}

package layout

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	KuraDir        = ".kura"
	KuraTrashDir   = "trash"
	SeriesFileName = "series.json"
	TrashFileName  = "trash.json"
)

func SeriesMetadataPath(seriesDir string) string {
	return filepath.Join(seriesDir, KuraDir, SeriesFileName)
}

func TrashMetadataPath(seriesDir string) string {
	return filepath.Join(seriesDir, KuraDir, TrashFileName)
}

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

func containsPath(root string, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func CleanSeriesRelPath(path string) (string, error) {
	clean, err := CleanRelPathAllowingKura(path)
	if err != nil {
		return "", err
	}
	if slices.Contains(strings.Split(clean, "/"), KuraDir) {
		return "", fmt.Errorf("path %q cannot point inside .kura", path)
	}
	return clean, nil
}

func CleanRelPathAllowingKura(path string) (string, error) {
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
	return filepath.ToSlash(clean), nil
}

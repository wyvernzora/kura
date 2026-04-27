package fsroot

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

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

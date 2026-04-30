package library

import (
	"errors"
	"path/filepath"
	"strings"
)

type Root struct {
	path string
}

func ParseRoot(path string) (Root, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Root{}, errors.New("library: root path is required")
	}
	return Root{path: filepath.Clean(path)}, nil
}

func (r Root) Path() string {
	return r.path
}

func (r Root) String() string {
	return r.path
}

func (r Root) Join(parts ...string) string {
	native := make([]string, 0, len(parts)+1)
	native = append(native, r.path)
	for _, part := range parts {
		native = append(native, filepath.FromSlash(part))
	}
	return filepath.Join(native...)
}

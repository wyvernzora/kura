package domain

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// SeriesPath is a direct child directory name below a library root.
type SeriesPath struct {
	name string
}

func ParseSeriesPath(name string) (SeriesPath, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return SeriesPath{}, errors.New("series path is required")
	}
	slashName := filepath.ToSlash(name)
	if filepath.IsAbs(name) || slashName != filepath.Base(slashName) {
		return SeriesPath{}, fmt.Errorf("series path %q must be a direct child of the library root", name)
	}
	if slashName == "." || slashName == ".." || slashName == ".kura" {
		return SeriesPath{}, fmt.Errorf("invalid series path %q", name)
	}
	if strings.ContainsFunc(slashName, func(r rune) bool {
		return unicode.IsControl(r) || r == '\t' || r == '\n' || r == '\r'
	}) {
		return SeriesPath{}, fmt.Errorf("invalid series path %q", name)
	}
	return SeriesPath{name: slashName}, nil
}

func (p SeriesPath) String() string {
	return p.name
}

func (p SeriesPath) IsZero() bool {
	return p.name == ""
}

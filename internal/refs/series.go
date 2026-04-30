package refs

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// Series identifies a tracked series by its library-root child directory.
type Series string

func ParseSeries(value string) (Series, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("series path is required")
	}
	slashName := filepath.ToSlash(value)
	if filepath.IsAbs(value) || slashName != filepath.Base(slashName) {
		return "", fmt.Errorf("series path %q must be a direct child of the library root", value)
	}
	if slashName == "." || slashName == ".." || slashName == ".kura" {
		return "", fmt.Errorf("invalid series path %q", value)
	}
	if strings.ContainsFunc(slashName, func(r rune) bool {
		return unicode.IsControl(r) || r == '\t' || r == '\n' || r == '\r'
	}) {
		return "", fmt.Errorf("invalid series path %q", value)
	}
	return Series(slashName), nil
}

func (ref Series) String() string {
	return string(ref)
}

func (ref Series) IsZero() bool {
	return ref == ""
}

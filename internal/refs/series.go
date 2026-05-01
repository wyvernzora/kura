package refs

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Series identifies a tracked series by directory name.
type Series string

func ParseSeries(value string) (Series, error) {
	value = norm.NFC.String(strings.TrimSpace(value))
	if value == "" {
		return "", errors.New("series name is required")
	}
	if value == "." || value == ".." || value == ".kura" {
		return "", fmt.Errorf("invalid series name %q", value)
	}
	if strings.ContainsFunc(value, func(r rune) bool {
		return r == '/' || r == '\\' || unicode.IsControl(r)
	}) {
		return "", fmt.Errorf("invalid series name %q", value)
	}
	return Series(value), nil
}

func (ref Series) String() string {
	return string(ref)
}

func (ref Series) IsZero() bool {
	return ref == ""
}

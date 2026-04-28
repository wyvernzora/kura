package domain

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// FileTitle is a normalized title used in generated folder and file names.
type FileTitle string

func ParseFileTitle(title string) (FileTitle, error) {
	clean := CleanFileTitle(title)
	if clean.IsZero() {
		return "", errors.New("library: filesystem title is required")
	}
	if invalidFileTitle(string(clean)) {
		return "", fmt.Errorf("library: invalid filesystem title %q", title)
	}
	return clean, nil
}

func CleanFileTitle(title string) FileTitle {
	return FileTitle(norm.NFC.String(strings.TrimSpace(title)))
}

func (t FileTitle) String() string {
	return string(t)
}

func (t FileTitle) IsZero() bool {
	return strings.TrimSpace(string(t)) == ""
}

func (t FileTitle) EqualName(name string) bool {
	return t == CleanFileTitle(name)
}

func invalidFileTitle(title string) bool {
	return strings.ContainsFunc(title, func(r rune) bool {
		return r == '/' || r == '\\' || r == 0 || unicode.IsControl(r)
	})
}

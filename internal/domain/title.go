package domain

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// FilesystemTitle is a normalized title used in generated folder and file names.
type FilesystemTitle string

func ParseFilesystemTitle(title string) (FilesystemTitle, error) {
	clean := CleanFilesystemTitle(title)
	if clean.IsZero() {
		return "", errors.New("library: filesystem title is required")
	}
	if invalidFilesystemTitle(string(clean)) {
		return "", fmt.Errorf("library: invalid filesystem title %q", title)
	}
	return clean, nil
}

func CleanFilesystemTitle(title string) FilesystemTitle {
	return FilesystemTitle(norm.NFC.String(strings.TrimSpace(title)))
}

func (t FilesystemTitle) String() string {
	return string(t)
}

func (t FilesystemTitle) IsZero() bool {
	return strings.TrimSpace(string(t)) == ""
}

func (t FilesystemTitle) EqualName(name string) bool {
	return t == CleanFilesystemTitle(name)
}

func invalidFilesystemTitle(title string) bool {
	return strings.ContainsFunc(title, func(r rune) bool {
		return r == '/' || r == '\\' || r == 0 || unicode.IsControl(r)
	})
}

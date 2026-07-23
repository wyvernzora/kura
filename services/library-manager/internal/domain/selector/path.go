// Package selector hosts kura's typed path locators. A Path carries a
// scheme tag (inbox / series) plus a forward-slash, NFC, traversal-
// safe relative path. Workflows resolve a Path against the matching
// root (KURA_INBOX_ROOT for inbox; series dir for series) at the
// filesystem boundary.
//
// Selectors are distinct from refs in domain/refs — refs identify
// things by ID (tvdb:370070, S01E03), selectors locate them by path.
// The two share the "<scheme>:<rest>" wire convention but operate
// differently: refs resolve via lookup, selectors via filesystem join.
package selector

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"golang.org/x/text/unicode/norm"
)

// Scheme is the closed set of recognized selector prefixes.
type Scheme string

const (
	// Inbox is the scheme for paths under KURA_INBOX_ROOT. Used for
	// stage media + extras inputs and inbox listing outputs.
	Inbox Scheme = "inbox"

	// Series is the scheme for paths inside a single series directory
	// (always scoped to the request's series ref). Used for stage
	// trash inputs and reconcile library-internal step paths.
	Series Scheme = "series"

	// Library is the scheme for paths inside the library root
	// (KURA_LIBRARY_ROOT). Used for emitting series-root locations
	// and other library-relative paths in responses. Selectors with
	// this scheme are output-only today — no workflow input accepts
	// them.
	Library Scheme = "library"
)

// Path is a "<scheme>:<rel>" wire-shape selector. Relative is
// forward-slash, leading-slash-free, "..-free", NFC-normalized,
// non-empty.
type Path struct {
	Scheme   Scheme
	Relative string
}

// Parse parses any registered scheme. Used at boundaries that accept
// multiple schemes (e.g. reconcile step.From which mixes inbox: and
// series:). Workflows that require a single scheme should call
// ParseInbox or ParseSeries instead.
func Parse(s string) (Path, error) {
	scheme, rel, err := splitScheme(s)
	if err != nil {
		return Path{}, err
	}
	if !knownScheme(scheme) {
		return Path{}, fmt.Errorf("selector: unknown scheme %q (want one of: inbox, series, library)", scheme)
	}
	cleaned, err := CleanRelative(rel)
	if err != nil {
		return Path{}, err
	}
	if cleaned == "" {
		return Path{}, fmt.Errorf("selector: empty relative in %q", s)
	}
	return Path{Scheme: scheme, Relative: cleaned}, nil
}

// ParseInbox parses and rejects anything but the inbox scheme.
func ParseInbox(s string) (Path, error) {
	p, err := Parse(s)
	if err != nil {
		return Path{}, err
	}
	if p.Scheme != Inbox {
		return Path{}, fmt.Errorf("selector: expected inbox: scheme, got %q", p.Scheme)
	}
	return p, nil
}

// ParseSeries parses and rejects anything but the series scheme.
func ParseSeries(s string) (Path, error) {
	p, err := Parse(s)
	if err != nil {
		return Path{}, err
	}
	if p.Scheme != Series {
		return Path{}, fmt.Errorf("selector: expected series: scheme, got %q", p.Scheme)
	}
	return p, nil
}

// String returns "<scheme>:<rel>". Empty for the zero value.
func (p Path) String() string {
	if p.Scheme == "" || p.Relative == "" {
		return ""
	}
	return string(p.Scheme) + ":" + p.Relative
}

// IsZero reports whether the path is the zero value.
func (p Path) IsZero() bool { return p.Scheme == "" && p.Relative == "" }

// Resolve joins the relative path against the supplied root. The
// caller picks the right root for the path's scheme — inbox root for
// Inbox, series directory for Series. Resolve does no validation
// beyond filesystem path joining; callers must EvalSymlinks +
// prefix-check before stat'ing if symlink escape is a concern.
func (p Path) Resolve(root string) string {
	return filepath.Join(root, filepath.FromSlash(p.Relative))
}

// MarshalJSON emits the selector as a JSON string. Zero value emits "".
func (p Path) MarshalJSON() ([]byte, error) {
	if p.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(p.String())
}

// UnmarshalJSON parses any registered scheme. Empty string decodes to
// the zero value (so omitempty fields round-trip).
func (p *Path) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == "" {
		*p = Path{}
		return nil
	}
	parsed, err := Parse(raw)
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}

// splitScheme splits "<scheme>:<rest>" on the first colon. Filenames
// with embedded colons end up entirely in rest.
func splitScheme(s string) (Scheme, string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", errors.New("selector: empty")
	}
	idx := strings.IndexByte(s, ':')
	if idx <= 0 {
		return "", "", fmt.Errorf("selector: missing scheme in %q (want <scheme>:<rel>)", s)
	}
	return Scheme(s[:idx]), s[idx+1:], nil
}

func knownScheme(s Scheme) bool {
	switch s {
	case Inbox, Series, Library:
		return true
	}
	return false
}

// CleanRelative validates and NFC-normalizes a forward-slash relative
// path. Empty input returns empty (used by walkers / lookups to mean
// "the root itself"). Backslashes, leading slashes, and traversal
// segments that escape the implicit root are rejected.
//
// Traversal-style failures return *PathOutsideRootError; structural
// failures (backslash, leading slash) return *InvalidPathError.
func CleanRelative(rel string) (string, error) {
	if rel == "" {
		return "", nil
	}
	if strings.ContainsRune(rel, '\\') {
		return "", &InvalidPathError{Path: rel, Reason: "backslash not allowed"}
	}
	if strings.HasPrefix(rel, "/") {
		return "", &InvalidPathError{Path: rel, Reason: "leading slash not allowed"}
	}
	cleaned := filepath.ToSlash(filepath.Clean(rel))
	if cleaned == "." {
		return "", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", &PathOutsideRootError{Path: rel}
	}
	return nfc(cleaned), nil
}

// nfc NFC-normalizes a path string without trimming whitespace —
// leading/trailing spaces in filenames are unusual but legal, and
// silently mangling them would surprise operators.
func nfc(s string) string { return norm.NFC.String(s) }

// PathOutsideRootError signals a relative path escapes its implicit
// root (typically via "..") or that a resolved path falls outside
// the registered root.
type PathOutsideRootError struct {
	Path string
}

func (e *PathOutsideRootError) Error() string {
	return fmt.Sprintf("selector: path %q escapes root", e.Path)
}

func (e *PathOutsideRootError) Kind() string     { return errkind.KindInvalidRef }
func (e *PathOutsideRootError) Category() string { return errkind.CategoryInvalidParams }
func (e *PathOutsideRootError) Data() map[string]any {
	return map[string]any{"path": e.Path}
}

// InvalidPathError signals a path failed structural validation
// (backslash, leading slash, etc.).
type InvalidPathError struct {
	Path   string
	Reason string
}

func (e *InvalidPathError) Error() string {
	return fmt.Sprintf("selector: invalid path %q: %s", e.Path, e.Reason)
}

func (e *InvalidPathError) Kind() string     { return errkind.KindInvalidRef }
func (e *InvalidPathError) Category() string { return errkind.CategoryInvalidParams }
func (e *InvalidPathError) Data() map[string]any {
	return map[string]any{"path": e.Path, "reason": e.Reason}
}

// Package filename composes the canonical media filename basename from a
// series title plus episode and media facts. Pure derivation; no IO.
//
// Path-prefix construction lives in internal/storage/paths
// (paths.EpisodeMediaRel composes "Season N/<basename>"). This package
// owns the basename only.
package filename

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/media"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/textnorm"
)

// MaxBasenameBytes caps a generated basename at the typical POSIX
// limit (ext4 / APFS / ZFS all cap at 255 bytes). BuildMedia enforces
// the cap by shrinking the title portion until the composed name fits.
const MaxBasenameBytes = 255

// Title is a normalized series title safe to embed in a filename.
type Title struct {
	value textnorm.NFCString
}

// ParseTitle returns a Title after sanitization. Errors when the
// sanitized form is empty (input was whitespace, dots, or only chars
// that sanitize to nothing).
func ParseTitle(title string) (Title, error) {
	clean := CleanTitle(title)
	if clean.IsZero() {
		return Title{}, errors.New("filename: title is required")
	}
	return clean, nil
}

// CleanTitle returns the sanitized form of title without erroring on
// empty results. Use ParseTitle when an empty result must be rejected.
func CleanTitle(title string) Title {
	return Title{value: textnorm.NFC(Sanitize(title))}
}

// Sanitize applies the canonical ruleset for embedding arbitrary text
// in a filename. Total transform; never errors. Order:
//
//  1. NFC normalize.
//  2. Replace path separators (/ \) with space.
//  3. Replace colon with space-hyphen (Finder remaps : in display).
//  4. Replace Windows-hostile chars (< > " | ? *) with space.
//  5. Strip C0/C1 control chars including DEL.
//  6. Collapse runs of Unicode whitespace to single ASCII space.
//  7. Trim leading/trailing whitespace and dots.
//
// POSIX focus: rules 2-3 prevent on-disk hostility on Linux + macOS;
// rule 4 is cheap insurance for cross-FS portability (NTFS / SMB).
// Reserved-name (CON / PRN / AUX) and 260-char-path enforcement are
// intentionally not applied.
func Sanitize(s string) string {
	s = textnorm.NFC(s).String()
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteString(sanitizeMapRune(r))
	}
	return strings.Trim(collapseSpaces(b.String()), " .")
}

// sanitizeMapRune is the per-rune classifier from Sanitize. Returns
// the replacement substring (empty drops the rune entirely).
func sanitizeMapRune(r rune) string {
	switch {
	case r == '/' || r == '\\':
		return " "
	case r == ':':
		return " -"
	case r == '<' || r == '>' || r == '"' || r == '|' || r == '?' || r == '*':
		return " "
	case unicode.IsControl(r):
		// Whitespace controls (tab / newline / CR) collapse to
		// space; non-whitespace controls (NUL / DEL / etc.) drop.
		if unicode.IsSpace(r) {
			return " "
		}
		return ""
	default:
		return string(r)
	}
}

// collapseSpaces flattens runs of Unicode whitespace into a single
// ASCII space.
func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return b.String()
}

func (t Title) String() string {
	return t.value.String()
}

func (t Title) IsZero() bool {
	return t.value.IsZero()
}

// Media is the canonical media filename basename, e.g.
// "Bookworm - S01E01 (WebRip 1080p).mkv".
type Media string

// Facts captures the source/resolution shorthand embedded in the
// canonical filename.
type Facts struct {
	Source     media.Source
	Resolution media.Resolution
}

// BuildMedia composes the canonical basename from title + episode + facts
// + file extension. Caps the result at MaxBasenameBytes by shrinking the
// title portion at a UTF-8 rune boundary; non-title parts (episode
// marker, facts, extension) are preserved verbatim.
func BuildMedia(title Title, episode refs.Episode, facts Facts, extension string) Media {
	source := facts.Source.Display()
	resolution := facts.Resolution.Display()
	if resolution == "" {
		resolution = "UnknownResolution"
	}
	titleStr := title.String()
	suffix := fmt.Sprintf(" - %s (%s %s)%s", episode.Marker(), source, resolution, extension)
	budget := MaxBasenameBytes - len(suffix)
	if budget < 1 {
		// Pathological: suffix alone overflows the cap. Truncate the
		// whole composed string at a rune boundary; degraded but
		// at least valid UTF-8.
		composed := titleStr + suffix
		return Media(truncateUTF8(composed, MaxBasenameBytes))
	}
	if len(titleStr) > budget {
		titleStr = strings.TrimRight(truncateUTF8(titleStr, budget), " .")
	}
	return Media(titleStr + suffix)
}

func (m Media) String() string {
	return string(m)
}

// truncateUTF8 returns s shortened to at most max bytes without
// splitting a UTF-8 rune. Returns s unchanged if it already fits.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Walk back from max until the prefix is valid UTF-8 (no
	// mid-rune cut). At most 4 iterations since UTF-8 runes are
	// 1-4 bytes.
	for end := max; end > 0; end-- {
		if utf8.ValidString(s[:end]) {
			return s[:end]
		}
	}
	return ""
}

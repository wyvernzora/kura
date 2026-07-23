// Package searchkey computes the per-series fuzzy-search blob shipped
// on `ListRow`. The blob is a deduplicated set of *flattened* alias
// strings — one source string per line, all whitespace and punctuation
// removed, NFKD-decomposed with combining marks dropped, lowercased.
// CJK runs survive intact.
//
// Why flat lines instead of split tokens: fuse.js with `ignoreLocation`
// fuzzy-substring-matches the query inside each line. Joining
// "Sousou no Frieren" → "sousounofrieren" lets typed shorthands
// ("oreimo", "madomagi", "sousou") fuzzy-substring-match a single
// line; tokenizing first would scatter the alias into independent
// tokens that no shorthand could span.
//
// The output is **never user-facing**: it is fed directly into
// fuse.js on the client. Lines whose flattened form equals the
// canonical or preferred display title are dropped (the client
// searches those fields directly).
package searchkey

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/wyvernzora/kura/services/library/internal/provider"
)

// TranslatedTitle mirrors the domain shape; declared here as a small
// pair to avoid a cyclic dependency on the series package. Callers
// hand any persisted lang+value pairs they want considered.
type TranslatedTitle struct {
	Language string
	Value    string
}

// Inputs bundles every candidate string the fold considers. Aliases
// is transient per-call data — typically the latest provider response;
// callers may pass nil when only persisted state is in scope (e.g. a
// CLI alias mutation). UserAliases is persisted user shorthands.
// PreferredLangs filters TranslatedTitles via BCP-47 base form
// (e.g. "ja", "en"); empty list disables the translation channel.
type Inputs struct {
	Canonical        string
	Preferred        string
	TranslatedTitles []TranslatedTitle
	Aliases          []provider.TitleEntry
	UserAliases      []string
	PreferredLangs   []string
}

// Compute folds Inputs into the persisted search blob. Output is a
// newline-joined sorted list of flattened alias lines (deterministic
// across runs). Empty when no candidate produces a line — the client
// falls back to matching the display title fields alone.
func Compute(in Inputs) string {
	displaySet := map[string]struct{}{}
	if v := flatten(in.Canonical); v != "" {
		displaySet[v] = struct{}{}
	}
	if v := flatten(in.Preferred); v != "" {
		displaySet[v] = struct{}{}
	}

	prefSet := map[string]struct{}{}
	for _, lang := range in.PreferredLangs {
		l := strings.ToLower(strings.TrimSpace(lang))
		if l == "" {
			continue
		}
		prefSet[l] = struct{}{}
	}

	out := map[string]struct{}{}
	add := func(value string) {
		flat := flatten(value)
		if flat == "" {
			return
		}
		if _, ok := displaySet[flat]; ok {
			return
		}
		out[flat] = struct{}{}
	}

	for _, alias := range in.Aliases {
		if !isLatinOnly(alias.Value) {
			continue
		}
		add(alias.Value)
	}
	for _, entry := range in.TranslatedTitles {
		lang := strings.ToLower(strings.TrimSpace(entry.Language))
		if _, ok := prefSet[lang]; !ok {
			continue
		}
		add(entry.Value)
	}
	for _, alias := range in.UserAliases {
		add(alias)
	}

	if len(out) == 0 {
		return ""
	}
	lines := make([]string, 0, len(out))
	for line := range out {
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// flatten normalizes a candidate string into a single fuse-searchable
// line. Recipe:
//
//   - NFKD-decompose so combining marks split off,
//   - drop combining marks (Mn category) entirely,
//   - drop everything that's not a letter or digit (whitespace,
//     punctuation, separators) — adjacency carries the search signal,
//   - lowercase.
//
// CJK runs stay intact (each CJK glyph is a letter). Empty / no-letter
// input returns empty.
func flatten(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	decomposed := norm.NFKD.String(value)
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// isLatinOnly returns true when every letter in s is in the Basic-
// Latin / Latin-1 / Latin-Extended ranges. Digits, punctuation, and
// whitespace are ignored. Used to gate provider aliases — CJK-only
// aliases are dropped because the canonical title already covers
// CJK queries.
func isLatinOnly(s string) bool {
	hasLetter := false
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		hasLetter = true
		if !unicode.In(r, unicode.Latin) {
			return false
		}
	}
	return hasLetter
}

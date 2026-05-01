package tvdb

import "fmt"

// Canonical TVDB v4 episode-ordering values (the `season-type` path segment
// on `/series/{id}/episodes/{season-type}`). Documented as examples in the
// v4 swagger; the set is stable in practice.
const (
	OrderingDefault   = "default"
	OrderingOfficial  = "official"
	OrderingDVD       = "dvd"
	OrderingAbsolute  = "absolute"
	OrderingAlternate = "alternate"
	OrderingRegional  = "regional"
)

var orderingSet = map[string]struct{}{
	OrderingDefault:   {},
	OrderingOfficial:  {},
	OrderingDVD:       {},
	OrderingAbsolute:  {},
	OrderingAlternate: {},
	OrderingRegional:  {},
}

// Orderings returns the canonical ordering values in stable order. Suitable
// for CLI flag enum lists and help text.
func Orderings() []string {
	return []string{
		OrderingDefault,
		OrderingOfficial,
		OrderingDVD,
		OrderingAbsolute,
		OrderingAlternate,
		OrderingRegional,
	}
}

// ParseOrdering validates an ordering string. Empty input is valid and
// returned unchanged (caller-side sentinel for "unset"). Unknown values
// produce an error. Canonical values are returned as-is.
func ParseOrdering(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if _, ok := orderingSet[s]; !ok {
		return "", fmt.Errorf("invalid ordering %q (allowed: %v)", s, Orderings())
	}
	return s, nil
}

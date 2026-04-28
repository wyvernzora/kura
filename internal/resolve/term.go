package resolve

import (
	"regexp"
	"strings"
)

// Term is a parsed user-supplied token split into an optional lowercase
// prefix and a non-empty value. Strategy matching decides how to interpret it.
type Term struct {
	Prefix string
	Value  string
}

// Query is a collection of search terms.
type Query struct {
	Terms []Term
}

var prefixPattern = regexp.MustCompile(`^([a-z0-9]+):(.+)$`)

// ParseTerm splits raw into a prefixed term when it has a lowercase
// alphanumeric prefix, otherwise treating it as free-form text.
func ParseTerm(raw string) Term {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Term{}
	}
	if match := prefixPattern.FindStringSubmatch(trimmed); match != nil {
		return Term{Prefix: match[1], Value: match[2]}
	}
	return Term{Value: trimmed}
}

// ParseQuery parses raw tokens into terms, skipping empty entries.
func ParseQuery(raw []string) Query {
	terms := make([]Term, 0, len(raw))
	for _, value := range raw {
		term := ParseTerm(value)
		if term == (Term{}) {
			continue
		}
		terms = append(terms, term)
	}
	return Query{Terms: terms}
}

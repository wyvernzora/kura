package refs

import (
	"regexp"
	"strings"
)

// Term is a parsed selector token split into an optional lowercase prefix and
// a non-empty value. Resolution decides how to interpret it.
type Term struct {
	Prefix string
	Value  string
}

func (t Term) String() string {
	if t.Prefix == "" {
		return t.Value
	}
	return t.Prefix + ":" + t.Value
}

// Selector is a collection of terms used to identify a series.
type Selector struct {
	Terms []Term
}

var prefixPattern = regexp.MustCompile(`^([a-z0-9]+):(.+)$`)

func ParseTerm(raw string) Term {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Term{}
	}
	if match := prefixPattern.FindStringSubmatch(trimmed); match != nil {
		if match[1] == "dir" {
			return Term{Value: trimmed}
		}
		return Term{Prefix: match[1], Value: match[2]}
	}
	return Term{Value: trimmed}
}

func ParseSelector(raw []string) Selector {
	terms := make([]Term, 0, len(raw))
	for _, value := range raw {
		term := ParseTerm(value)
		if term == (Term{}) {
			continue
		}
		terms = append(terms, term)
	}
	return Selector{Terms: terms}
}

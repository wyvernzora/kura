package refs

import (
	"strings"

	"github.com/wyvernzora/kura/internal/textnorm"
)

// Term is a normalized selector token. Resolution strategies decide how to
// interpret it.
type Term string

func (t Term) String() string {
	return string(t)
}

// Selector is a collection of terms used to identify a series.
type Selector struct {
	Terms []Term
}

func ParseTerm(raw string) Term {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Term("")
	}
	return Term(textnorm.NFC(trimmed).String())
}

func ParseSelector(raw []string) Selector {
	terms := make([]Term, 0, len(raw))
	for _, value := range raw {
		term := ParseTerm(value)
		if term == "" {
			continue
		}
		terms = append(terms, term)
	}
	return Selector{Terms: terms}
}

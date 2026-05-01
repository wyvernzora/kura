package resolve

import "github.com/wyvernzora/kura/internal/refs"

// Term is normalized selector text. Strategy matching decides how to parse and
// interpret it.
type Term = refs.Term

// Query is a collection of search terms.
type Query = refs.Selector

// ParseTerm normalizes raw selector text. Strategies decide how to parse and
// interpret the term.
func ParseTerm(raw string) Term {
	return refs.ParseTerm(raw)
}

// ParseQuery parses raw tokens into terms, skipping empty entries.
func ParseQuery(raw []string) Query {
	return refs.ParseSelector(raw)
}

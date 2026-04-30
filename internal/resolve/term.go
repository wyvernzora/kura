package resolve

import "github.com/wyvernzora/kura/internal/refs"

// Term is a parsed user-supplied token split into an optional lowercase
// prefix and a non-empty value. Strategy matching decides how to interpret it.
type Term = refs.Term

// Query is a collection of search terms.
type Query = refs.Selector

// ParseTerm splits raw into a prefixed term when it has a lowercase
// alphanumeric prefix, otherwise treating it as free-form text.
func ParseTerm(raw string) Term {
	return refs.ParseTerm(raw)
}

// ParseQuery parses raw tokens into terms, skipping empty entries.
func ParseQuery(raw []string) Query {
	return refs.ParseSelector(raw)
}

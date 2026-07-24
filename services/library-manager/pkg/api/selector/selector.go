// Package selector is the public wire/vocabulary facade over internal/domain/selector;
// the internal package remains the implementation.
package selector

import iselector "github.com/wyvernzora/kura/services/library-manager/internal/domain/selector"

type (
	Path   = iselector.Path
	Term   = iselector.Term
	Scheme = iselector.Scheme
)

const (
	Inbox  = iselector.Inbox
	Series = iselector.Series
)

func Parse(value string) (Path, error) {
	return iselector.Parse(value)
}

func ParseTerm(value string) Term {
	return iselector.ParseTerm(value)
}

func ParseInbox(value string) (Path, error) {
	return iselector.ParseInbox(value)
}

func ParseSeries(value string) (Path, error) {
	return iselector.ParseSeries(value)
}

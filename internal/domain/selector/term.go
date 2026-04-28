package selector

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

func ParseTerm(raw string) Term {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Term("")
	}
	return Term(textnorm.NFC(trimmed).String())
}

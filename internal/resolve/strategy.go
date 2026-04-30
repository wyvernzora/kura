package resolve

import (
	"context"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/metadata"
)

// ResolveStrategy is the unit of term-resolution behavior. Strategies hold
// their own dependencies, identify matching terms, and return term-level hits.
type ResolveStrategy interface {
	// Name reports a stable identifier for telemetry.
	Name() string

	// Match reports whether this strategy handles the given term.
	Match(term Term) bool

	// Authoritative reports whether matching terms must be sole query terms,
	// modulo same-value duplicates.
	Authoritative() bool

	// Resolve produces this term's candidate hits. An empty slice with nil error
	// is a normal term-level not-found outcome.
	Resolve(ctx context.Context, term Term) ([]termHit, error)
}

// termHit is one term's contribution for one metadata candidate.
type termHit struct {
	Term        Term
	MetadataRef domain.MetadataRef
	Summary     metadata.SeriesSummary
	Rank        int
}

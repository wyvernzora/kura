package resolve

import "context"

// ResolveStrategy is the unit of term-resolution behavior. Strategies hold
// their own dependencies, identify matching terms, and return term-level hits.
type ResolveStrategy interface {
	// Name reports a stable identifier for evidence labelling and telemetry.
	Name() string

	// Match reports whether this strategy handles the given term.
	Match(term Term) bool

	// Authoritative reports whether matching terms must be sole query terms,
	// modulo same-value duplicates.
	Authoritative() bool

	// Resolve produces this term's candidate hits. An empty slice with nil error
	// is a normal term-level not-found outcome.
	Resolve(ctx context.Context, term Term) ([]TermHit, error)
}

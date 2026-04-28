package resolve

import "github.com/wyvernzora/kura/internal/metadata"

// TermHit is one term's contribution for one provider candidate.
type TermHit struct {
	Term        Term
	Strategy    string
	ProviderRef string
	Summary     metadata.SeriesSummary
	Rank        int
}

// Resolution is the resolver's success-path output. Outcome is encoded by
// result cardinality: zero is not found, one is resolved, many is unresolved.
type Resolution struct {
	Results []Result
}

func (r Resolution) IsResolved() bool {
	return len(r.Results) == 1
}

func (r Resolution) IsUnresolved() bool {
	return len(r.Results) > 1
}

func (r Resolution) IsNotFound() bool {
	return len(r.Results) == 0
}

// Result is one provider candidate with all evidence supporting it.
type Result struct {
	Summary  metadata.SeriesSummary
	Evidence []TermHit
}

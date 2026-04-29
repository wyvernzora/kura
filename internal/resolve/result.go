package resolve

import "github.com/wyvernzora/kura/internal/metadata"

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

// Result is one metadata candidate with all evidence supporting it.
type Result struct {
	Summary  metadata.SeriesSummary
	Evidence []Evidence
}

// Evidence is one term's visible contribution to a result.
type Evidence struct {
	Term string
	Rank int
}

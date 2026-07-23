package resolve

import "github.com/wyvernzora/kura/services/library-manager/internal/provider"

// Resolution is the resolver's success-path output. Outcome is encoded by
// result cardinality: zero is not found, one is resolved, many is unresolved.
type Resolution struct {
	Results []Result
}

// Result is one metadata candidate with all evidence supporting it.
type Result struct {
	Summary  provider.SeriesSummary
	Evidence []Evidence
}

// Evidence is one term's visible contribution to a result.
type Evidence struct {
	Term        string
	Rank        int
	MatchSource string   `json:",omitempty"`
	Annotations []string `json:",omitempty"`
}

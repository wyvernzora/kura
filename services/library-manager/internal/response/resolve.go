package response

import "github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"

// Resolution is the workflow.Resolve return shape. Outcome is encoded by
// candidate-list cardinality:
//
//   - len(Candidates) == 0 → not found
//   - len(Candidates) == 1 → unique match
//   - len(Candidates) >  1 → ambiguous (caller picks)
//
// Workflow does not pick. CLI helpers may auto-promote a single candidate
// or prompt on multi; agents inspect the slice and decide.
type Resolution struct {
	Candidates []Candidate `json:"candidates"`
}

// Candidate is one provider match. Fields are flat strings (no NFCString
// or other textnorm types) so MCP and CLI --json output can serialize
// uniformly. Add fields here only when at least one surface needs them.
type Candidate struct {
	Ref                refs.Metadata `json:"ref"`
	PreferredTitle     string        `json:"preferredTitle"`
	CanonicalTitle     string        `json:"canonicalTitle,omitempty"`
	Year               int           `json:"year,omitempty"`
	FirstAired         string        `json:"firstAired,omitempty"`
	OriginalLanguage   string        `json:"originalLanguage,omitempty"`
	OriginalCountry    string        `json:"originalCountry,omitempty"`
	Genres             []string      `json:"genres,omitempty"`
	PosterURL          string        `json:"posterUrl,omitempty"`
	PosterThumbnailURL string        `json:"posterThumbnailUrl,omitempty"`
	Evidence           []Evidence    `json:"evidence,omitempty"`
}

// Evidence is one term's contribution to a candidate. Surfaces use
// it for ranking heuristics: which term matched, where in the
// provider record, and any qualifying annotations (e.g. full_match
// vs partial_match).
type Evidence struct {
	Term        string   `json:"term"`
	Rank        int      `json:"rank"`
	MatchSource string   `json:"matchSource,omitempty"`
	Annotations []string `json:"annotations,omitempty"`
}

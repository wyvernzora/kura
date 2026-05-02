package response

import "github.com/wyvernzora/kura/internal/domain/refs"

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
	Ref              refs.Metadata `json:"ref"`
	PreferredTitle   string        `json:"preferredTitle"`
	CanonicalTitle   string        `json:"canonicalTitle,omitempty"`
	Year             int           `json:"year,omitempty"`
	FirstAired       string        `json:"firstAired,omitempty"`
	OriginalLanguage string        `json:"originalLanguage,omitempty"`
	OriginalCountry  string        `json:"originalCountry,omitempty"`
}

package response

import "github.com/wyvernzora/kura/internal/domain/refs"

// AddResult is workflow.Add's response. The caller already supplied
// the metadata ref and knows the library root; the only information
// this surface adds is the sanitized on-disk directory basename and
// the provider's preferred display title. Callers wanting full state
// follow with workflow.Show.
type AddResult struct {
	Ref            refs.Series `json:"ref"`
	PreferredTitle string      `json:"preferredTitle"`
}

// ImportResult is workflow.Import's response. Same shape as AddResult
// for now; surfaces can render either with the same template.
type ImportResult = AddResult

package api

import "github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"

// AddResult is workflow.Add's response. Ref (the metadata ref) is
// echoed because the surface caller (CLI / script) often resolved it
// from text terms rather than passing it directly — the resolved ref
// is genuinely new info to them. Directory is the sanitized on-disk
// basename (non-trivial to derive from arbitrary titles);
// PreferredTitle is the provider's display string. Library root is
// implicit and dropped.
type AddResult struct {
	Ref            refs.Metadata `json:"ref"`
	Directory      refs.Series   `json:"directory"`
	PreferredTitle string        `json:"preferredTitle"`
}

// ImportResult is workflow.Import's response. Same shape as AddResult
// for now; surfaces can render either with the same template.
type ImportResult = AddResult

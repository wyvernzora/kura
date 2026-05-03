package response

import "github.com/wyvernzora/kura/internal/domain/refs"

// AddResult is workflow.Add's response. MetadataRef is echoed because
// the surface caller (CLI / script) often resolved it from text terms
// rather than passing it directly — the resolved ref is genuinely new
// info to them. The on-disk Ref is the sanitized directory basename
// (non-trivial to derive from arbitrary titles); PreferredTitle is the
// provider's display string. Library root is implicit and dropped.
type AddResult struct {
	MetadataRef    refs.Metadata `json:"metadataRef"`
	Ref            refs.Series   `json:"ref"`
	PreferredTitle string        `json:"preferredTitle"`
}

// ImportResult is workflow.Import's response. Same shape as AddResult
// for now; surfaces can render either with the same template.
type ImportResult = AddResult

package response

import "github.com/wyvernzora/kura/internal/domain/refs"

// AddResult is workflow.Add's response. The series is freshly created;
// callers typically follow with kura_show for the full state.
type AddResult struct {
	MetadataRef    refs.Metadata `json:"metadataRef"`
	Ref            refs.Series   `json:"ref"`
	Root           string        `json:"root"`
	PreferredTitle string        `json:"preferredTitle"`
}

// ImportResult is workflow.Import's response. Same shape as AddResult
// for now; surfaces can render either with the same template.
type ImportResult = AddResult

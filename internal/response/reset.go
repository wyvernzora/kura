package response

import "github.com/wyvernzora/kura/internal/domain/refs"

// ResetResult is workflow.Reset's response. The Episode + Record fields
// are populated when a single staged record is dropped; the Records
// slice is populated when --all drops every staged record at once.
type ResetResult struct {
	Series  refs.Series   `json:"series"`
	Applied bool          `json:"applied"`
	Episode *refs.Episode `json:"episode,omitempty"`
	Record  *MediaShow    `json:"record,omitempty"`
	Records []ResetRecord `json:"records,omitempty"`
}

type ResetRecord struct {
	Episode refs.Episode `json:"episode"`
	Record  MediaShow    `json:"record"`
}

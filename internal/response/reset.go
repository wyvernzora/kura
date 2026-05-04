package response

import "github.com/wyvernzora/kura/internal/domain/refs"

// ResetResult is workflow.Reset's response. Single-mode (caller passed
// one Episode) returns just the dropped Record; the episode is echoed
// from the input. --all mode returns the per-episode list of dropped
// records so the caller learns what got cleared. Empty Records on
// --all means there was nothing staged.
type ResetResult struct {
	Record  *MediaShow    `json:"record,omitempty"`
	Records []ResetRecord `json:"records,omitempty"`
}

type ResetRecord struct {
	Episode refs.Episode `json:"episode"`
	Record  MediaShow    `json:"record"`
}

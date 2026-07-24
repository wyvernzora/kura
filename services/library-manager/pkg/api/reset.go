package api

import "github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"

// ResetResult is workflow.Reset's response. Single-mode (caller passed
// one Episode) returns just the dropped Record; the episode is echoed
// from the input. --all mode returns the per-episode list of dropped
// records so the caller learns what got cleared. Empty Records on
// --all means there was nothing staged.
//
// TrashRemoved and ExtraRemoved list ULIDs of stagedTrash / stagedExtras
// entries that were dropped — populated when caller passes TrashIDs /
// ExtraIDs in ResetInput, or when All=true.
type ResetResult struct {
	Record       *MediaShow    `json:"record,omitempty"`
	Records      []ResetRecord `json:"records,omitempty"`
	TrashRemoved []string      `json:"trashRemoved,omitempty"`
	ExtraRemoved []string      `json:"extraRemoved,omitempty"`
}

type ResetRecord struct {
	Episode refs.Episode `json:"episode"`
	Record  MediaShow    `json:"record"`
}

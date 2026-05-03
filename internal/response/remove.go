package response

import "github.com/wyvernzora/kura/internal/domain/refs"

// RemoveMode describes which slice of the series got deleted.
type RemoveMode string

const (
	// RemoveModeUntrack drops .kura/ metadata only; media files stay.
	RemoveModeUntrack RemoveMode = "untrack"
	// RemoveModePurge wholesale deletes the entire series directory.
	RemoveModePurge RemoveMode = "purge"
)

// Remove is workflow.Remove's response.
type Remove struct {
	Ref            refs.Series `json:"ref"`
	Mode           RemoveMode  `json:"mode"`
	ReclaimedBytes int64       `json:"reclaimedBytes"`
}

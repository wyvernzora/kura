package response

import "github.com/wyvernzora/kura/services/library/internal/domain/refs"

// StageResult is workflow.Stage's response. Stage may queue a mix of
// episode stages, trash items, and extras in one batch; results surface
// per-item so the caller (CLI renderer or MCP client) can present a
// status table.
//
// Rows that survived Phase 1 input validation but failed Phase 2 work
// (mediainfo probe, file vanished mid-flight) appear in Skipped[]. Rows
// that succeeded appear in Episodes / Trash / Extras with status =
// "staged" (legacy single-item shape used "replaced" — surface that as
// status="replaced" for episode rows when the new stage displaced an
// active record).
type StageResult struct {
	Episodes []StageEpisodeResult `json:"episodes"`
	Trash    []StageTrashResult   `json:"trash"`
	Extras   []StageExtraResult   `json:"extras"`
	Skipped  []StageSkip          `json:"skipped"`
}

// StageEpisodeResult is one episode item that landed in series.json.
// Status is one of: "staged" (slot was empty), "replaced" (displaced
// active record; old file will be trashed on reconcile_apply).
type StageEpisodeResult struct {
	Episode refs.Episode `json:"episode"`
	Status  string       `json:"status"`
	Record  MediaShow    `json:"record"`
}

// StageTrashResult is one trash item queued in stagedTrash[].
type StageTrashResult struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// StageExtraResult is one extras item queued in stagedExtras[].
type StageExtraResult struct {
	ID     string `json:"id"`
	Season int    `json:"season"`
	Path   string `json:"path"`
	Prefix string `json:"prefix,omitempty"`
}

// StageSkip is one item that survived Phase 1 input validation but
// failed Phase 2 work. Code is a stable token (probe_failed,
// source_missing, etc.); Reason is the underlying error message.
type StageSkip struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

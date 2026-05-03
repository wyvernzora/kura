package response

import "github.com/wyvernzora/kura/internal/domain/refs"

// ScanStatus is the per-episode outcome of a scan pass.
type ScanStatus string

const (
	ScanStatusAdded     ScanStatus = "added"
	ScanStatusReplaced  ScanStatus = "replaced"
	ScanStatusUpdated   ScanStatus = "updated"
	ScanStatusUnchanged ScanStatus = "unchanged"
	ScanStatusRemoved   ScanStatus = "removed"
)

// ScanResult is workflow.Scan's response. Synced is the per-episode
// reconciliation log (what changed); Skipped is the list of files /
// directories the scan ignored with reasons; OrphanSlots are episode
// refs the local model still tracks (because they hold active or
// staged records) but the provider no longer knows about.
type ScanResult struct {
	Series      refs.Series      `json:"series"`
	Synced      []ScannedEpisode `json:"synced"`
	Skipped     []ScanSkip       `json:"skipped"`
	OrphanSlots []refs.Episode   `json:"orphanSlots"`
}

// ScannedEpisode mirrors what changed for one episode slot during a
// scan pass: what status (added/updated/...), which episode, and the
// media facts of the new (or removed) record.
type ScannedEpisode struct {
	Status     ScanStatus   `json:"status"`
	Episode    refs.Episode `json:"episode"`
	Source     string       `json:"source"`
	Resolution string       `json:"resolution,omitempty"`
	Path       string       `json:"path"`
	Companions []string     `json:"companions"`
}

// ScanSkip is one file or directory the scan walked past with an
// explanation. Codes mirror the scan-internal SkipCode* constants.
type ScanSkip struct {
	Path   string `json:"path"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

// Skip code constants. Exported so renderers and MCP schemas can
// reference them by name.
const (
	SkipCodeSpecialNumberNotInferred = "special_number_not_inferred"
	SkipCodeEpisodeNumberNotInferred = "episode_number_not_inferred"
	SkipCodeSeasonMismatch           = "season_mismatch"
	SkipCodeIgnoredDirectory         = "ignored_directory"
	SkipCodeDuplicateSlot            = "duplicate_slot"
)

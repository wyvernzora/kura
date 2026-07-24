package api

// InboxList is workflow.InboxList's response. Path is an `inbox:<rel>`
// selector identifying the sub-path that was listed; empty means the
// inbox root itself.
//
// When Truncated is true the slice was capped to the caller's Limit
// (default 500, max 5000). ElidedCount carries how many entries were
// dropped. Hint contains human-readable suggestions for narrowing the
// next request.
type InboxList struct {
	Path        string       `json:"path"`
	Entries     []InboxEntry `json:"entries"`
	Truncated   bool         `json:"truncated,omitempty"`
	ElidedCount int          `json:"elidedCount,omitempty"`
	Hint        []string     `json:"hint,omitempty"`
}

// InboxEntry is one filesystem entry under the inbox root. Path is
// an `inbox:<rel>` selector — pass it straight back to kura_stage as
// the `media` arg. Size is meaningful only for files; dirs and
// symlinks emit 0. SymlinkTarget is populated only when Kind ==
// "symlink" and is the symlink's literal target string (may point
// outside any known root).
type InboxEntry struct {
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	Size          int64  `json:"size,omitempty"`
	MTime         string `json:"mtime,omitempty"`
	SymlinkTarget string `json:"symlinkTarget,omitempty"`
}

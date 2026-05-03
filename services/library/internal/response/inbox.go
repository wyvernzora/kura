package response

// InboxList is workflow.InboxList's response. Path is the (cleaned,
// NFC) sub-path that was listed; empty means the inbox root itself.
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
// forward-slash, NFC, relative to the inbox root. The basename is
// filepath.Base(Path); the inbox: selector form is "inbox:" + Path —
// clients can derive both trivially when needed. Size is meaningful
// only for files; dirs and symlinks emit 0. SymlinkTarget is populated
// only when Kind == "symlink".
type InboxEntry struct {
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	Size          int64  `json:"size,omitempty"`
	MTime         string `json:"mtime,omitempty"`
	SymlinkTarget string `json:"symlinkTarget,omitempty"`
}

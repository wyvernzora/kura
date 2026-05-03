package response

// Remove is workflow.Remove's response. Caller knew the series ref
// and whether they passed --purge; only the reclaimed-bytes count is
// new info.
type Remove struct {
	ReclaimedBytes int64 `json:"reclaimedBytes"`
}

package api

// ReindexResult is the terminal payload of POST /api/library/v1/reindex.
// Surfaces the row count so the caller can sanity-check the rebuild
// against expectations without re-fetching the full list.
type ReindexResult struct {
	// Items is the total number of rows the rebuild emitted (tracked +
	// untracked + error). Matches `len(index.Rows())` post-write.
	Items int `json:"items"`
}

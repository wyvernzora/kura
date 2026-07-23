package response

// ReindexResult is the terminal payload of POST /api/v1/library/reindex.
// Surfaces the row count so the caller can sanity-check the rebuild
// against expectations without re-fetching the full list.
type ReindexResult struct {
	// Rows is the total number of rows the rebuild emitted (tracked +
	// untracked + error). Matches `len(index.Rows())` post-write.
	Rows int `json:"rows"`
}

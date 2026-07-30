package api

import "time"

// CrawlRequest is the POST /api/v1/sources/{source}/crawl body: consume
// exactly pageSize posts from the cursor's resume point, ingest them
// directly, and return the next cursor. The API remains one bounded chunk per
// request and holds no crawl state between requests; the kura CLI threads
// chunks automatically and exposes the cursor as a recovery checkpoint.
type CrawlRequest struct {
	// PageSize is the exact number of posts to consume, from 1 through 200.
	// Zero means 200.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is the opaque resume point from a previous response; empty
	// starts at the newest listing.
	Cursor string `json:"cursor,omitempty"`
	// Lookback is an extended Go duration (12h, 30d, 2w, 2w12h); pages with
	// no plausible in-window date end the walk with hasMore=false. Empty
	// means no limit — the walk runs to the archive floor.
	Lookback string `json:"lookback,omitempty"`
}

// Stop reasons reported on CrawlResult.StopReason.
const (
	CrawlStopPageBudget       = "page_budget"
	CrawlStopLookbackBoundary = "lookback_boundary"
	CrawlStopArchiveFloor     = "archive_floor"
)

// CrawlResult reports one chunk: what was crawled, what ingestion did with
// it, and the resume state. NextCursor is present exactly when HasMore is
// true; loop until hasMore is false.
type CrawlResult struct {
	Source string `json:"source"`
	// Posts is the number of posts consumed and handed to ingestion.
	Posts int `json:"posts"`
	// PagesFetched counts upstream listing pages read this chunk (cache
	// hits included).
	PagesFetched int `json:"pagesFetched"`
	// Batch and Queue mirror the POST /api/v1/releases/ingest response.
	Batch IngestBatch `json:"batch"`
	Queue QueueCounts `json:"queue"`
	// OldestPublishedAt / NewestPublishedAt bound the consumed posts'
	// non-zero stamps; omitted when no post carried a date.
	OldestPublishedAt *time.Time `json:"oldestPublishedAt,omitempty"`
	NewestPublishedAt *time.Time `json:"newestPublishedAt,omitempty"`
	NextCursor        string     `json:"nextCursor,omitempty"`
	HasMore           bool       `json:"hasMore"`
	StopReason        string     `json:"stopReason"`
}

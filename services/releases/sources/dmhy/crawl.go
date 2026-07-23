package dmhy

import (
	"context"
	"time"

	"github.com/wyvernzora/takuhai/internal/metrics"
	"github.com/wyvernzora/takuhai/pkg/crawl"
	"github.com/wyvernzora/takuhai/pkg/rawpost"
)

// PageFetcher fetches the raw bytes for a 1-based DMHY page number and sort_id.
type PageFetcher func(ctx context.Context, sortID, page int) (body []byte, err error)

// CrawlRequest is the POST /crawl request body.
type CrawlRequest = crawl.CrawlRequest

// CrawlResponse is the POST /crawl response body.
type CrawlResponse struct {
	Posts      []rawpost.RawPost `json:"posts"`
	NextCursor string            `json:"next_cursor"`
	HasMore    bool              `json:"has_more"`

	stopReason   string `json:"-"`
	pagesFetched int    `json:"-"`
	lastPage     int    `json:"-"`
}

// Crawler is the stateless DMHY crawl engine behind POST /crawl.
type Crawler struct {
	fetch     PageFetcher
	threshold int
	sortID    int
	now       func() time.Time
	metrics   *metrics.Crawler
}

// NewCrawler constructs a stateless crawler over a page fetcher and the
// consecutive-empty threshold N. sortID defaults to 0 (the bare-path archive walk);
// the clock defaults to time.Now.
func NewCrawler(fetch PageFetcher, threshold int) *Crawler {
	return &Crawler{fetch: fetch, threshold: threshold, sortID: 0, now: time.Now}
}

func (c *Crawler) Crawl(ctx context.Context, req CrawlRequest, lookback time.Duration) (CrawlResponse, error) {
	resp, err := c.shared().Crawl(ctx, req, lookback)
	if err != nil {
		return CrawlResponse{}, err
	}
	return crawlResponse(resp), nil
}

func crawlResponse(resp crawl.CrawlResponse) CrawlResponse {
	return CrawlResponse{
		Posts:        resp.Posts,
		NextCursor:   resp.NextCursor,
		HasMore:      resp.HasMore,
		stopReason:   resp.StopReason,
		pagesFetched: resp.PagesFetched,
		lastPage:     resp.LastPage,
	}
}

func parseCursor(cursor string) (page, offset int, err error) {
	return crawl.ParseCursor("dmhy", cursor)
}

func formatCursor(page, offset int) string {
	return crawl.FormatCursor(page, offset)
}

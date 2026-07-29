package dmhy

import (
	"context"
	"fmt"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/crawl"
)

// PageFetcher fetches the raw bytes for a 1-based DMHY page number.
type PageFetcher func(ctx context.Context, page int) (body []byte, err error)

// Crawler walks DMHY's newest archive pages.
type Crawler struct {
	fetch     PageFetcher
	threshold int
	category  int
}

// NewCrawler constructs a crawler over a page fetcher.
func NewCrawler(fetch PageFetcher, threshold int) *Crawler {
	return &Crawler{fetch: fetch, threshold: threshold}
}

// Crawl returns up to limit of the newest DMHY posts.
func (c *Crawler) Crawl(ctx context.Context, limit int) ([]api.RawPost, error) {
	return c.shared().Crawl(ctx, limit)
}

// CrawlSince walks DMHY's archive newest-first, emitting each page, until the
// listing is provably older than oldest or the archive floor is reached.
func (c *Crawler) CrawlSince(ctx context.Context, oldest time.Time, emit func([]api.RawPost) error) error {
	return c.shared().CrawlSince(ctx, oldest, emit)
}

// CrawlChunk consumes exactly pageSize posts from the cursor's resume point,
// per the chunked backfill contract behind POST /api/v1/sources/dmhy/crawl.
func (c *Crawler) CrawlChunk(ctx context.Context, pageSize int, cursor string, lookback time.Duration) (crawl.CrawlResponse, error) {
	return c.shared().CrawlChunk(ctx, pageSize, cursor, lookback)
}

func (c *Crawler) shared() *crawl.Crawler {
	return crawl.NewCrawler(crawl.Config{
		Source:    "dmhy",
		Fetch:     crawl.PageFetcher(c.fetch),
		Parse:     ParseArchivePage,
		Threshold: c.threshold,
		ParseErrorContext: func(page int) string {
			return fmtParseContext(c.category, page)
		},
	})
}

func fmtParseContext(category, page int) string {
	return fmt.Sprintf("category %d page %d", category, page)
}

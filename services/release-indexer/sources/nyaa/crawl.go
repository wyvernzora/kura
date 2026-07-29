package nyaa

import (
	"context"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/crawl"
)

// PageFetcher fetches the raw HTML bytes for a 1-based Nyaa result page.
type PageFetcher func(ctx context.Context, page int) (body []byte, err error)

// Crawler walks Nyaa's newest listing pages.
type Crawler struct {
	fetch     PageFetcher
	threshold int
}

// NewCrawler constructs a crawler over a page fetcher.
func NewCrawler(fetch PageFetcher, threshold int) *Crawler {
	return &Crawler{fetch: fetch, threshold: threshold}
}

// Crawl returns up to limit of the newest Nyaa posts.
func (c *Crawler) Crawl(ctx context.Context, limit int) ([]api.RawPost, error) {
	return c.shared().Crawl(ctx, limit)
}

// CrawlSince walks Nyaa's listing newest-first, emitting each page, until the
// listing is provably older than oldest or the feed floor is reached.
func (c *Crawler) CrawlSince(ctx context.Context, oldest time.Time, emit func([]api.RawPost) error) error {
	return c.shared().CrawlSince(ctx, oldest, emit)
}

// Page fetches and parses one 1-based Nyaa result page.
func (c *Crawler) Page(ctx context.Context, page int) ([]api.RawPost, error) {
	return c.shared().Page(ctx, page)
}

func (c *Crawler) shared() *crawl.Crawler {
	return crawl.NewCrawler(crawl.Config{
		Source:    "nyaa",
		Fetch:     crawl.PageFetcher(c.fetch),
		Parse:     ParseListingPage,
		Threshold: c.threshold,
	})
}

package nyaa

import (
	"context"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/crawl"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/rawpost"
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
func (c *Crawler) Crawl(ctx context.Context, limit int) ([]rawpost.RawPost, error) {
	return crawl.NewCrawler(crawl.Config{
		Source:    "nyaa",
		Fetch:     crawl.PageFetcher(c.fetch),
		Parse:     ParseListingPage,
		Threshold: c.threshold,
	}).Crawl(ctx, limit)
}

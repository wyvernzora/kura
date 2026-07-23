package dmhy

import (
	"context"
	"fmt"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/crawl"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/rawpost"
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
func (c *Crawler) Crawl(ctx context.Context, limit int) ([]rawpost.RawPost, error) {
	return c.shared().Crawl(ctx, limit)
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

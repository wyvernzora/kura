package dmhy

import (
	"fmt"
	"strings"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/crawl"
)

// Threshold is the DMHY consecutive-empty archive-floor threshold.
const Threshold = 2

func archivePageURL(base string, category, page int) string {
	b := strings.TrimRight(base, "/")
	if category > 0 {
		return fmt.Sprintf("%s/topics/list/sort_id/%d/page/%d", b, category, page)
	}
	return fmt.Sprintf("%s/topics/list/page/%d", b, page)
}

// NewHTTPCrawler constructs a DMHY crawler over a live HTTP/file source.
func NewHTTPCrawler(baseURL string, category int, maxRPS float64, cacheTTL time.Duration) *Crawler {
	c := &Crawler{threshold: Threshold, category: category}
	fetcher := crawl.NewHTTPFetcher(crawl.HTTPFetcherConfig{
		Source: "dmhy",
		BuildURL: func(page int) (string, error) {
			return archivePageURL(baseURL, c.category, page), nil
		},
		RatePerSec: maxRPS,
	})
	fetch := PageFetcher(fetcher.FetchPage)
	if cacheTTL > 0 {
		fetch = newPageCache(cacheTTL).wrap(fetch)
	}
	c.fetch = fetch
	return c
}

package nyaa

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/crawl"
)

// Threshold is the Nyaa consecutive-empty feed-floor threshold.
const Threshold = 2

// NewHTTPCrawler constructs a Nyaa crawler over a live HTTP/file source.
func NewHTTPCrawler(baseURL, query, category, filter string, maxRPS float64, requestTimeout, cacheTTL time.Duration) *Crawler {
	fetcher := crawl.NewHTTPFetcher(crawl.HTTPFetcherConfig{
		Source: "nyaa",
		BuildURL: func(page int) (string, error) {
			return listingPageURL(baseURL, query, category, filter, page)
		},
		RatePerSec:     maxRPS,
		RequestTimeout: requestTimeout,
	})
	fetch := crawl.PageFetcher(fetcher.FetchPage)
	if cacheTTL > 0 {
		fetch = crawl.NewPageCache(cacheTTL).Wrap(fetch)
	}
	return &Crawler{
		fetch:     PageFetcher(fetch),
		threshold: Threshold,
	}
}

func listingPageURL(base, search, category, filter string, page int) (string, error) {
	if strings.HasPrefix(base, "file://") {
		return fmt.Sprintf("%s/page-%d.html", strings.TrimRight(base, "/"), page), nil
	}
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return "", fmt.Errorf("nyaa: parse base url %q: %w", base, err)
	}
	if u.Path == "" {
		u.Path = "/"
	}
	q := u.Query()
	q.Set("p", strconv.Itoa(page))
	if search != "" {
		q.Set("q", search)
	}
	if category != "" {
		q.Set("c", category)
	}
	if filter != "" {
		q.Set("f", filter)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

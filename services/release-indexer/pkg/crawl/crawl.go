package crawl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// PageFetcher fetches the raw bytes for a 1-based page number.
type PageFetcher func(ctx context.Context, page int) (body []byte, err error)

// PageParser parses one fetched page into newest-to-oldest raw posts.
type PageParser func(body []byte) ([]api.RawPost, error)

// Config wires a source crawler.
type Config struct {
	Source            string
	Fetch             PageFetcher
	Parse             PageParser
	Threshold         int
	ParseErrorContext func(page int) string
}

// Crawler walks a source from its newest page until it fills the requested
// limit or confirms the source's consecutive-empty archive floor.
type Crawler struct {
	source            string
	fetch             PageFetcher
	parse             PageParser
	threshold         int
	parseErrorContext func(page int) string
}

// NewCrawler constructs a crawler over a page fetcher and parser.
func NewCrawler(cfg Config) *Crawler {
	return &Crawler{
		source:            cfg.Source,
		fetch:             cfg.Fetch,
		parse:             cfg.Parse,
		threshold:         cfg.Threshold,
		parseErrorContext: cfg.ParseErrorContext,
	}
}

var (
	// ErrCrawlFetch marks an upstream page fetch failure.
	ErrCrawlFetch = errors.New("crawl fetch")
	// ErrCrawlParse marks a fetched page that could not be parsed.
	ErrCrawlParse = errors.New("crawl parse")
)

// Crawl returns up to limit of the newest posts. A fetch or parse failure
// returns no partial batch so a later scheduled run can retry from page one.
func (c *Crawler) Crawl(ctx context.Context, limit int) ([]api.RawPost, error) {
	limit = clampLimit(limit)

	posts := make([]api.RawPost, 0, limit)
	consecutiveEmpty := 0
	for page := 1; ; page++ {
		body, err := c.fetch(ctx, page)
		if err != nil {
			return nil, fmt.Errorf("%s: %w: %w", c.source, ErrCrawlFetch, err)
		}
		pagePosts, err := c.parsePage(body, page)
		if err != nil {
			return nil, err
		}

		if len(pagePosts) == 0 {
			consecutiveEmpty++
			if consecutiveEmpty >= c.threshold {
				return posts, nil
			}
			continue
		}
		consecutiveEmpty = 0

		remaining := limit - len(posts)
		if len(pagePosts) >= remaining {
			return append(posts, pagePosts[:remaining]...), nil
		}
		posts = append(posts, pagePosts...)
	}
}

// Page fetches and parses one 1-based listing page.
func (c *Crawler) Page(ctx context.Context, page int) ([]api.RawPost, error) {
	body, err := c.fetch(ctx, page)
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %w", c.source, ErrCrawlFetch, err)
	}
	return c.parsePage(body, page)
}

const (
	// plausibleGuardFactor bounds how far behind `oldest` a stamp may sit and
	// still drive stop math: stamps older than guardFactor × (now − oldest)
	// before now are treated as artifacts (epoch-zero renders, pinned ancient
	// rows) rather than listing position.
	plausibleGuardFactor = 10
	// plausibleFutureSlack tolerates small clock skew; stamps further in the
	// future never drive stop math (they would keep the walk from ever
	// stopping), though their posts are still emitted.
	plausibleFutureSlack = 5 * time.Minute
)

// ErrNoPlausibleDates marks a walk that kept finding posts whose dates are
// all missing or implausible — parser or markup breakage, surfaced instead of
// walking to the archive floor on every run.
var ErrNoPlausibleDates = errors.New("crawl: no plausible dates")

// CrawlSince walks listing pages newest-first, invoking emit for every parsed
// page, until the listing is provably older than oldest or the source's
// consecutive-empty archive floor is reached. Each page is emitted before the
// stop rule is evaluated, so the boundary page is always delivered.
//
// The stop rule uses the page's NEWEST plausible stamp: a pinned/sticky old
// row or an epoch-zero artifact can lower a page's minimum but never its
// maximum, so a single anomalous row cannot terminate the walk early. Posts
// with implausible stamps are still emitted — they just never drive the stop.
func (c *Crawler) CrawlSince(ctx context.Context, oldest time.Time, emit func([]api.RawPost) error) error {
	now := time.Now()
	floor := oldest.Add(-plausibleGuardFactor * now.Sub(oldest))
	ceil := now.Add(plausibleFutureSlack)

	consecutiveEmpty := 0
	consecutiveUndatable := 0
	for page := 1; ; page++ {
		pagePosts, err := c.Page(ctx, page)
		if err != nil {
			return err
		}

		if len(pagePosts) == 0 {
			consecutiveEmpty++
			if consecutiveEmpty >= c.threshold {
				return nil
			}
			continue
		}
		consecutiveEmpty = 0

		if err := emit(pagePosts); err != nil {
			return fmt.Errorf("%s: emit page %d: %w", c.source, page, err)
		}

		newest := newestPlausible(pagePosts, floor, ceil)
		if newest.IsZero() {
			consecutiveUndatable++
			if consecutiveUndatable >= c.threshold {
				return fmt.Errorf("%s: %w: %d consecutive pages ending at page %d", c.source, ErrNoPlausibleDates, consecutiveUndatable, page)
			}
			continue
		}
		consecutiveUndatable = 0

		if !newest.After(oldest) {
			return nil
		}
	}
}

func newestPlausible(posts []api.RawPost, floor, ceil time.Time) time.Time {
	var newest time.Time
	for _, p := range posts {
		t := p.PublishedAt
		if t.Before(floor) || t.After(ceil) {
			continue
		}
		if t.After(newest) {
			newest = t
		}
	}
	return newest
}

func (c *Crawler) parsePage(body []byte, page int) ([]api.RawPost, error) {
	pagePosts, err := c.parse(body)
	if err == nil {
		return pagePosts, nil
	}
	contextText := ""
	if c.parseErrorContext != nil {
		contextText = c.parseErrorContext(page)
	}
	if contextText != "" {
		return nil, fmt.Errorf("%s: %w: %s: %w", c.source, ErrCrawlParse, contextText, err)
	}
	return nil, fmt.Errorf("%s: %w: page %d: %w", c.source, ErrCrawlParse, page, err)
}

func clampLimit(n int) int {
	if n < 1 {
		return 1
	}
	if n > 200 {
		return 200
	}
	return n
}

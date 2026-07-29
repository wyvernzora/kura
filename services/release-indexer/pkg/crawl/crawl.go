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
	// Now is the injectable clock the lookback cutoff reads; nil means
	// time.Now.
	Now func() time.Time
}

// Crawler walks a source from its newest page until it fills the requested
// limit or confirms the source's consecutive-empty archive floor.
type Crawler struct {
	source            string
	fetch             PageFetcher
	parse             PageParser
	threshold         int
	parseErrorContext func(page int) string
	now               func() time.Time
}

// NewCrawler constructs a crawler over a page fetcher and parser.
func NewCrawler(cfg Config) *Crawler {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Crawler{
		source:            cfg.Source,
		fetch:             cfg.Fetch,
		parse:             cfg.Parse,
		threshold:         cfg.Threshold,
		parseErrorContext: cfg.ParseErrorContext,
		now:               now,
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

// plausibleFutureSlack tolerates small clock skew; stamps further in the
// future never drive stop math (they would keep the walk from ever stopping),
// though their posts are still emitted.
const plausibleFutureSlack = 5 * time.Minute

// plausibleEpochFloor rejects epoch/zero render artifacts — Nyaa renders
// data-timestamp="0" as a parseable 1970 stamp, and Go's zero Time renders as
// year 1 — as stop-math inputs. It is deliberately ABSOLUTE rather than
// relative to the settle window: a window-relative floor mis-classifies in
// both directions (a page definitively older than the boundary — the
// strongest possible stop signal — reads as undatable under a short window,
// while a 1970 artifact reads as plausible under a long one) and its
// multiplication overflows int64 for multi-decade windows. Both sources
// postdate this floor by years, so a stamp at or before it is markup, never
// listing position.
var plausibleEpochFloor = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

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
// A page carrying no plausible stamp at all is undatable; consecutive
// undatable pages mean parser or markup breakage and fail the walk.
func (c *Crawler) CrawlSince(ctx context.Context, oldest time.Time, emit func([]api.RawPost) error) error {
	ceil := time.Now().Add(plausibleFutureSlack)

	consecutiveEmpty := 0
	consecutiveUndatable := 0
	for page := 1; ; page++ {
		pagePosts, err := c.Page(ctx, page)
		if err != nil {
			return err
		}

		if len(pagePosts) == 0 {
			consecutiveEmpty++
			consecutiveUndatable = 0
			if consecutiveEmpty >= c.threshold {
				return nil
			}
			continue
		}
		consecutiveEmpty = 0

		if err := emit(pagePosts); err != nil {
			return fmt.Errorf("%s: emit page %d: %w", c.source, page, err)
		}

		newest := newestPlausible(pagePosts, ceil)
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

func newestPlausible(posts []api.RawPost, ceil time.Time) time.Time {
	var newest time.Time
	for _, p := range posts {
		t := p.PublishedAt
		if !plausibleDate(t, ceil) {
			continue
		}
		if t.After(newest) {
			newest = t
		}
	}
	return newest
}

func plausibleDate(t, ceil time.Time) bool {
	return t.After(plausibleEpochFloor) && !t.After(ceil)
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

package crawl

import (
	"context"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/rawpost"
)

// PageFetcher fetches the raw bytes for a 1-based page number.
type PageFetcher func(ctx context.Context, page int) (body []byte, err error)

// PageParser parses one fetched page into newest-to-oldest raw posts.
type PageParser func(body []byte) ([]rawpost.RawPost, error)

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
func (c *Crawler) Crawl(ctx context.Context, limit int) ([]rawpost.RawPost, error) {
	limit = clampLimit(limit)

	posts := make([]rawpost.RawPost, 0, limit)
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

func (c *Crawler) parsePage(body []byte, page int) ([]rawpost.RawPost, error) {
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

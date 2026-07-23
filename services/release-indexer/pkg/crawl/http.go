package crawl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// HTTPFetcher fetches source pages over HTTP or file:// fixtures.
type HTTPFetcher struct {
	source      string
	buildURL    func(page int) (string, error)
	limiter     *rate.Limiter
	client      *http.Client
	readFileURL func(path string) ([]byte, error)
}

// HTTPFetcherConfig wires HTTP/file fetching for one source.
type HTTPFetcherConfig struct {
	Source     string
	BuildURL   func(page int) (string, error)
	RatePerSec float64
	Client     *http.Client
}

// NewHTTPFetcher constructs a PageFetcher over HTTP and file:// URLs.
func NewHTTPFetcher(cfg HTTPFetcherConfig) *HTTPFetcher {
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	f := &HTTPFetcher{
		source:   cfg.Source,
		buildURL: cfg.BuildURL,
		client:   client,
		readFileURL: func(path string) ([]byte, error) {
			return ReadFileURL(cfg.Source, path)
		},
	}
	if cfg.RatePerSec > 0 {
		f.limiter = rate.NewLimiter(rate.Limit(cfg.RatePerSec), 1)
	}
	return f
}

// FetchPage fetches one 1-based page.
func (f *HTTPFetcher) FetchPage(ctx context.Context, page int) ([]byte, error) {
	if f.limiter != nil {
		if err := f.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("%s: rate limiter: %w", f.source, err)
		}
	}

	target, err := f.buildURL(page)
	if err != nil {
		return nil, err
	}

	if rest, ok := strings.CutPrefix(target, "file://"); ok {
		return f.readFileURL(rest)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%s: build request %s: %w", f.source, target, err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: fetch %s: %w", f.source, target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: fetch %s: status %d", f.source, target, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read body %s: %w", f.source, target, err)
	}
	return b, nil
}

// ReadFileURL reads a file:// page body for fixture-backed tests.
func ReadFileURL(source, path string) ([]byte, error) {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: read file %s: %w", source, path, err)
	}
	return b, nil
}

package crawl

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	retryMaxAttempts    = 3
	retryInitialBackoff = 2 * time.Second
	retryMaxBackoff     = 30 * time.Second
	latencyWindow       = 3
)

// HTTPFetcher fetches source pages over HTTP or file:// fixtures.
type HTTPFetcher struct {
	source      string
	buildURL    func(page int) (string, error)
	limiter     *rate.Limiter
	client      *http.Client
	readFileURL func(path string) ([]byte, error)
	logger      *slog.Logger
	gate        chan struct{}
	now         func() time.Time
	sleep       func(context.Context, time.Duration) error

	lastCompleted time.Time
	durations     [latencyWindow]time.Duration
	durationNext  int
	durationLen   int
}

// HTTPFetcherConfig wires HTTP/file fetching for one source.
type HTTPFetcherConfig struct {
	Source     string
	BuildURL   func(page int) (string, error)
	RatePerSec float64
	// RequestTimeout bounds one page fetch when no Client is injected.
	// Deep-history pages on SQL-backed sources can exceed 60s (observed on
	// DMHY), so sources configure this rather than inheriting the old
	// hardcoded 30s. Zero keeps the 30s default.
	RequestTimeout time.Duration
	Client         *http.Client
	Logger         *slog.Logger

	// Test seams for deterministic pacing tests.
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

// NewHTTPFetcher constructs a PageFetcher over HTTP and file:// URLs.
func NewHTTPFetcher(cfg HTTPFetcherConfig) *HTTPFetcher {
	client := cfg.Client
	if client == nil {
		timeout := cfg.RequestTimeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	now := cfg.now
	if now == nil {
		now = time.Now
	}
	sleep := cfg.sleep
	if sleep == nil {
		sleep = sleepContext
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	f := &HTTPFetcher{
		source:   cfg.Source,
		buildURL: cfg.BuildURL,
		client:   client,
		logger:   logger,
		gate:     make(chan struct{}, 1),
		now:      now,
		sleep:    sleep,
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
	target, err := f.buildURL(page)
	if err != nil {
		return nil, err
	}

	if err := f.acquire(ctx); err != nil {
		return nil, fmt.Errorf("%s: wait for fetch slot: %w", f.source, err)
	}
	defer f.release()

	var (
		lastErr      error
		attemptsUsed int
	)
	for attempt := 1; attempt <= retryMaxAttempts; attempt++ {
		attemptsUsed = attempt
		if err := f.waitForTurn(ctx); err != nil {
			return nil, fmt.Errorf("%s: pace fetch: %w", f.source, err)
		}

		started := f.now()
		body, retryAfter, retryable, err := f.fetchOnce(ctx, target)
		f.observe(f.now().Sub(started))
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retryable || attempt == retryMaxAttempts {
			break
		}

		delay := retryBackoff(attempt)
		if retryAfter > delay {
			delay = retryAfter
		}
		if delay > retryMaxBackoff {
			delay = retryMaxBackoff
		}
		f.logger.WarnContext(ctx, "source page fetch retrying",
			"source", f.source,
			"page", page,
			"attempt", attempt,
			"max_attempts", retryMaxAttempts,
			"backoff_ms", delay.Milliseconds(),
			"err", err,
		)
		if err := f.sleep(ctx, delay); err != nil {
			return nil, fmt.Errorf("%s: retry page %d: %w", f.source, page, err)
		}
	}

	if attemptsUsed == 1 {
		return nil, lastErr
	}
	return nil, fmt.Errorf("%s: fetch page %d failed after %d attempts: %w",
		f.source, page, attemptsUsed, lastErr)
}

func (f *HTTPFetcher) fetchOnce(
	ctx context.Context,
	target string,
) (body []byte, retryAfter time.Duration, retryable bool, err error) {
	if rest, ok := strings.CutPrefix(target, "file://"); ok {
		body, err := f.readFileURL(rest)
		return body, 0, false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	if err != nil {
		return nil, 0, false, fmt.Errorf("%s: build request %s: %w", f.source, target, err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, 0, ctx.Err() == nil, fmt.Errorf("%s: fetch %s: %w", f.source, target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, parseRetryAfter(resp.Header.Get("Retry-After"), f.now()),
			retryableStatus(resp.StatusCode),
			fmt.Errorf("%s: fetch %s: status %d", f.source, target, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, ctx.Err() == nil, fmt.Errorf("%s: read body %s: %w", f.source, target, err)
	}
	return b, 0, false, nil
}

func (f *HTTPFetcher) acquire(ctx context.Context) error {
	select {
	case f.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *HTTPFetcher) release() {
	<-f.gate
}

func (f *HTTPFetcher) waitForTurn(ctx context.Context) error {
	if !f.lastCompleted.IsZero() {
		wait := f.lastCompleted.Add(f.averageDuration()).Sub(f.now())
		if wait > 0 {
			if err := f.sleep(ctx, wait); err != nil {
				return err
			}
		}
	}
	if f.limiter != nil {
		if err := f.limiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limiter: %w", err)
		}
	}
	return nil
}

func (f *HTTPFetcher) observe(duration time.Duration) {
	if duration < 0 {
		duration = 0
	}
	f.lastCompleted = f.now()
	f.durations[f.durationNext] = duration
	f.durationNext = (f.durationNext + 1) % len(f.durations)
	if f.durationLen < len(f.durations) {
		f.durationLen++
	}
}

func (f *HTTPFetcher) averageDuration() time.Duration {
	if f.durationLen == 0 {
		return 0
	}
	var total time.Duration
	for i := 0; i < f.durationLen; i++ {
		total += f.durations[i]
	}
	return total / time.Duration(f.durationLen)
}

func retryableStatus(status int) bool {
	return status == http.StatusRequestTimeout ||
		status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError
}

func retryBackoff(failedAttempt int) time.Duration {
	delay := retryInitialBackoff
	for i := 1; i < failedAttempt; i++ {
		if delay >= retryMaxBackoff/2 {
			return retryMaxBackoff
		}
		delay *= 2
	}
	if delay > retryMaxBackoff {
		return retryMaxBackoff
	}
	return delay
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds >= int64(retryMaxBackoff/time.Second) {
			return retryMaxBackoff
		}
		return time.Duration(seconds) * time.Second
	}
	at, err := http.ParseTime(raw)
	if err != nil || !at.After(now) {
		return 0
	}
	return at.Sub(now)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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

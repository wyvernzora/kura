package crawl

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPFetcherRetriesTransientFailures(t *testing.T) {
	var attempts atomic.Int32
	var slept []time.Duration
	fetcher := testHTTPFetcher(roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempt := attempts.Add(1)
		if attempt < 3 {
			return response(http.StatusServiceUnavailable, "unavailable"), nil
		}
		return response(http.StatusOK, "ok"), nil
	}), func(_ context.Context, delay time.Duration) error {
		slept = append(slept, delay)
		return nil
	})

	body, err := fetcher.FetchPage(t.Context(), 1)
	if err != nil {
		t.Fatalf("FetchPage() error = %v", err)
	}
	if string(body) != "ok" || attempts.Load() != 3 {
		t.Fatalf("body = %q, attempts = %d", body, attempts.Load())
	}
	if len(slept) != 2 || slept[0] != 2*time.Second || slept[1] != 4*time.Second {
		t.Fatalf("retry sleeps = %v, want [2s 4s]", slept)
	}
}

func TestHTTPFetcherDoesNotRetryPermanentStatus(t *testing.T) {
	var attempts atomic.Int32
	fetcher := testHTTPFetcher(roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts.Add(1)
		return response(http.StatusBadRequest, "bad request"), nil
	}), func(context.Context, time.Duration) error {
		t.Fatal("unexpected sleep")
		return nil
	})

	_, err := fetcher.FetchPage(t.Context(), 1)
	if err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("FetchPage() error = %v, want status 400", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1", attempts.Load())
	}
}

func TestHTTPFetcherCapsRetryAfter(t *testing.T) {
	var attempts atomic.Int32
	var slept []time.Duration
	fetcher := testHTTPFetcher(roundTripFunc(func(*http.Request) (*http.Response, error) {
		if attempts.Add(1) == 1 {
			resp := response(http.StatusTooManyRequests, "slow down")
			resp.Header.Set("Retry-After", "120")
			return resp, nil
		}
		return response(http.StatusOK, "ok"), nil
	}), func(_ context.Context, delay time.Duration) error {
		slept = append(slept, delay)
		return nil
	})

	if _, err := fetcher.FetchPage(t.Context(), 1); err != nil {
		t.Fatalf("FetchPage() error = %v", err)
	}
	if len(slept) != 1 || slept[0] != retryMaxBackoff {
		t.Fatalf("sleeps = %v, want [%s]", slept, retryMaxBackoff)
	}
}

func TestHTTPFetcherRetryBackoffHonorsContext(t *testing.T) {
	var attempts atomic.Int32
	ctx, cancel := context.WithCancel(t.Context())
	fetcher := testHTTPFetcher(roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts.Add(1)
		return response(http.StatusServiceUnavailable, "unavailable"), nil
	}), func(ctx context.Context, delay time.Duration) error {
		cancel()
		return sleepContext(ctx, delay)
	})

	_, err := fetcher.FetchPage(ctx, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchPage() error = %v, want context canceled", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1", attempts.Load())
	}
}

func TestHTTPFetcherWaitsForRollingLatencyAverage(t *testing.T) {
	var mu sync.Mutex
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	durations := []time.Duration{3 * time.Second, 6 * time.Second, 9 * time.Second, time.Second}
	var request int
	var slept []time.Duration

	fetcher := NewHTTPFetcher(HTTPFetcherConfig{
		Source:   "test",
		BuildURL: func(int) (string, error) { return "https://example.test", nil },
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			mu.Lock()
			now = now.Add(durations[request])
			request++
			mu.Unlock()
			return response(http.StatusOK, "ok"), nil
		})},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		now: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return now
		},
		sleep: func(_ context.Context, delay time.Duration) error {
			mu.Lock()
			slept = append(slept, delay)
			now = now.Add(delay)
			mu.Unlock()
			return nil
		},
	})

	for page := 1; page <= len(durations); page++ {
		if _, err := fetcher.FetchPage(t.Context(), page); err != nil {
			t.Fatalf("FetchPage(%d) error = %v", page, err)
		}
	}
	want := []time.Duration{3 * time.Second, 4500 * time.Millisecond, 6 * time.Second}
	if len(slept) != len(want) {
		t.Fatalf("sleeps = %v, want %v", slept, want)
	}
	for i := range want {
		if slept[i] != want[i] {
			t.Fatalf("sleep %d = %s, want %s", i, slept[i], want[i])
		}
	}
}

func TestHTTPFetcherSerializesConcurrentRequests(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	fetcher := testHTTPFetcher(roundTripFunc(func(*http.Request) (*http.Response, error) {
		current := inFlight.Add(1)
		if current > maxInFlight.Load() {
			maxInFlight.Store(current)
		}
		entered <- struct{}{}
		<-release
		inFlight.Add(-1)
		return response(http.StatusOK, "ok"), nil
	}), func(context.Context, time.Duration) error { return nil })

	errs := make(chan error, 2)
	go func() {
		_, err := fetcher.FetchPage(t.Context(), 1)
		errs <- err
	}()
	<-entered
	go func() {
		_, err := fetcher.FetchPage(t.Context(), 2)
		errs <- err
	}()

	select {
	case <-entered:
		t.Fatal("second request entered transport before first completed")
	case <-time.After(20 * time.Millisecond):
	}
	release <- struct{}{}
	<-entered
	release <- struct{}{}
	if err := <-errs; err != nil {
		t.Fatalf("first FetchPage() error = %v", err)
	}
	if err := <-errs; err != nil {
		t.Fatalf("second FetchPage() error = %v", err)
	}
	if maxInFlight.Load() != 1 {
		t.Fatalf("max in-flight = %d, want 1", maxInFlight.Load())
	}
}

func testHTTPFetcher(transport http.RoundTripper, sleep func(context.Context, time.Duration) error) *HTTPFetcher {
	fixedNow := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	return NewHTTPFetcher(HTTPFetcherConfig{
		Source:   "test",
		BuildURL: func(int) (string, error) { return "https://example.test", nil },
		Client:   &http.Client{Transport: transport},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		now:      func() time.Time { return fixedNow },
		sleep:    sleep,
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

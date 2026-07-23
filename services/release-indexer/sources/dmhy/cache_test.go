package dmhy

import (
	"context"
	"errors"
	"testing"
	"time"
)

// countingFetcher returns a PageFetcher that tallies upstream calls and a fixed body.
func countingFetcher(calls *int, body []byte, err error) PageFetcher {
	return func(context.Context, int) ([]byte, error) {
		*calls++
		return body, err
	}
}

// TestPageCacheDedupsWithinTTL: repeated fetches of the same page within the TTL hit DMHY
// exactly once.
func TestPageCacheDedupsWithinTTL(t *testing.T) {
	var calls int
	c := newPageCache(10 * time.Minute)
	now := time.Unix(0, 0)
	c.now = func() time.Time { return now }
	w := c.wrap(countingFetcher(&calls, []byte("page"), nil))

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		b, err := w(ctx, 1)
		if err != nil || string(b) != "page" {
			t.Fatalf("call %d: body=%q err=%v", i, b, err)
		}
	}
	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1 (cached within TTL)", calls)
	}
}

// TestPageCachePagesDistinct: each page has its own cache entry.
func TestPageCachePagesDistinct(t *testing.T) {
	var calls int
	c := newPageCache(time.Minute)
	now := time.Unix(0, 0)
	c.now = func() time.Time { return now }
	w := c.wrap(countingFetcher(&calls, []byte("x"), nil))

	ctx := context.Background()
	w(ctx, 1)
	w(ctx, 2)
	w(ctx, 1)
	if calls != 2 {
		t.Fatalf("upstream calls = %d, want 2 (distinct pages; one repeat cached)", calls)
	}
}

// TestPageCacheExpires: once the TTL elapses, the page is re-fetched.
func TestPageCacheExpires(t *testing.T) {
	var calls int
	c := newPageCache(10 * time.Minute)
	now := time.Unix(0, 0)
	c.now = func() time.Time { return now }
	w := c.wrap(countingFetcher(&calls, []byte("x"), nil))

	ctx := context.Background()
	w(ctx, 1) // fetch (calls=1)
	now = now.Add(5 * time.Minute)
	w(ctx, 1)                      // cached (calls=1)
	now = now.Add(6 * time.Minute) // 11m total > 10m TTL
	w(ctx, 1)                      // expired -> refetch (calls=2)
	if calls != 2 {
		t.Fatalf("upstream calls = %d, want 2 (refetch after TTL)", calls)
	}
}

// TestPageCacheDoesNotCacheErrors: a failed fetch is never cached, so it stays
// immediately retryable.
func TestPageCacheDoesNotCacheErrors(t *testing.T) {
	var calls int
	c := newPageCache(time.Minute)
	now := time.Unix(0, 0)
	c.now = func() time.Time { return now }
	w := c.wrap(countingFetcher(&calls, nil, errors.New("boom")))

	ctx := context.Background()
	if _, err := w(ctx, 1); err == nil {
		t.Fatal("want error from failing fetch")
	}
	if _, err := w(ctx, 1); err == nil {
		t.Fatal("want error from failing fetch")
	}
	if calls != 2 {
		t.Fatalf("upstream calls = %d, want 2 (errors not cached)", calls)
	}
}

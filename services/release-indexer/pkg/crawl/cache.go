package crawl

import (
	"context"
	"sync"
	"time"
)

// PageCache is a small TTL cache over a PageFetcher. It reuses a completed
// page fetch within ttl, sparing slow sources repeated fetches and making
// chunked crawls cheap — a chunk
// cursor parked mid-page means the next chunk re-reads that page, and within
// the TTL it re-reads the SAME snapshot, so continuation is deterministic.
//
// It caches EVERY page including the newest — within ttl a page's bytes are
// frozen, so brand-new posts on the frontier are not seen until the entry
// expires. Keep ttl at or below the poll interval if frontier freshness
// matters.
//
// Only successful fetches are cached; a fetch error is never cached, so a
// transient upstream blip stays immediately retryable.
//
// ponytail: a plain mutex-guarded map with sweep-on-insert. The entry count
// is bounded by the crawler's rate limit × ttl, so no LRU/size cap is needed
// at this scale.
type PageCache struct {
	ttl   time.Duration
	now   func() time.Time // injectable clock; real builds use time.Now
	mu    sync.Mutex
	items map[int]pageEntry
}

type pageEntry struct {
	body    []byte
	expires time.Time
}

// NewPageCache constructs a page cache with the given TTL.
func NewPageCache(ttl time.Duration) *PageCache {
	return &PageCache{ttl: ttl, now: time.Now, items: make(map[int]pageEntry)}
}

// Wrap returns a PageFetcher that serves fresh cache hits and otherwise
// delegates to fetch, caching successful results under the page number.
func (c *PageCache) Wrap(fetch PageFetcher) PageFetcher {
	return func(ctx context.Context, page int) ([]byte, error) {
		if body, ok := c.get(page); ok {
			return body, nil
		}
		body, err := fetch(ctx, page)
		if err != nil {
			return nil, err // never cache a failure
		}
		c.put(page, body)
		return body, nil
	}
}

func (c *PageCache) get(page int) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[page]
	if !ok || !c.now().Before(e.expires) {
		return nil, false
	}
	return e.body, true
}

func (c *PageCache) put(page int, body []byte) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	// Sweep expired entries so the map stays bounded to pages fetched within
	// the ttl.
	for k, e := range c.items {
		if !now.Before(e.expires) {
			delete(c.items, k)
		}
	}
	c.items[page] = pageEntry{body: body, expires: now.Add(c.ttl)}
}

package crawl

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// sinceFixture serves synthetic pages: pages[i] is the list of publishedAt
// offsets (relative to now, negative = past) for page i+1; pages beyond the
// slice are empty. Bodies are page numbers; the parser fabricates posts.
type sinceFixture struct {
	now   time.Time
	pages [][]time.Duration
}

func (f *sinceFixture) fetch(_ context.Context, page int) ([]byte, error) {
	return []byte(strconv.Itoa(page)), nil
}

func (f *sinceFixture) parse(body []byte) ([]api.RawPost, error) {
	page, err := strconv.Atoi(string(body))
	if err != nil {
		return nil, err
	}
	if page > len(f.pages) {
		return nil, nil
	}
	var posts []api.RawPost
	for i, offset := range f.pages[page-1] {
		posts = append(posts, api.RawPost{
			Source:      "test",
			SourceID:    fmt.Sprintf("p%d-%d", page, i),
			Title:       "post",
			Magnet:      "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
			PublishedAt: f.now.Add(offset),
		})
	}
	return posts, nil
}

func (f *sinceFixture) crawler() *Crawler {
	return NewCrawler(Config{
		Source:    "test",
		Fetch:     f.fetch,
		Parse:     f.parse,
		Threshold: 2,
	})
}

func repeatOffsets(n int, offset time.Duration) []time.Duration {
	out := make([]time.Duration, n)
	for i := range out {
		out[i] = offset - time.Duration(i)*time.Second
	}
	return out
}

// The motivating incident: a backlog far beyond the old 200-post cap, inside
// the settle window, must be fully emitted page by page in one walk.
func TestCrawlSinceWalksEntireBacklog(t *testing.T) {
	now := time.Now()
	fixture := &sinceFixture{now: now, pages: [][]time.Duration{
		repeatOffsets(80, -1*time.Hour),
		repeatOffsets(80, -50*time.Hour),
		repeatOffsets(80, -100*time.Hour),
		repeatOffsets(80, -200*time.Hour), // older than oldest: boundary page
	}}

	var emitted [][]api.RawPost
	err := fixture.crawler().CrawlSince(t.Context(), now.Add(-150*time.Hour), func(posts []api.RawPost) error {
		emitted = append(emitted, posts)
		return nil
	})
	if err != nil {
		t.Fatalf("CrawlSince() error = %v", err)
	}
	// All four pages emitted (320 posts > the old 200 cap), including the
	// boundary page, then the walk stops without touching page 5.
	if len(emitted) != 4 {
		t.Fatalf("emitted %d pages, want 4", len(emitted))
	}
	total := 0
	for _, page := range emitted {
		total += len(page)
	}
	if total != 320 {
		t.Fatalf("emitted %d posts, want 320", total)
	}
}

// A pinned/sticky old row (plausible but ancient relative to the page) sits
// on page 1; the stop rule anchors on the page's NEWEST plausible stamp, so
// the walk must continue past it.
func TestCrawlSinceIgnoresStickyOldRowForStop(t *testing.T) {
	now := time.Now()
	pageOne := append([]time.Duration{-72 * time.Hour}, repeatOffsets(10, -1*time.Hour)...)
	fixture := &sinceFixture{now: now, pages: [][]time.Duration{
		pageOne,
		repeatOffsets(10, -30*time.Hour), // boundary: newest stamp ≤ oldest
	}}

	pages := 0
	err := fixture.crawler().CrawlSince(t.Context(), now.Add(-24*time.Hour), func([]api.RawPost) error {
		pages++
		return nil
	})
	if err != nil {
		t.Fatalf("CrawlSince() error = %v", err)
	}
	if pages != 2 {
		t.Fatalf("walked %d pages, want 2 (sticky old row on page 1 must not stop the walk)", pages)
	}
}

// Epoch-zero artifacts (Nyaa renders data-timestamp="0" as a parseable 1970
// stamp) and future stamps are implausible: they are emitted but never drive
// the stop rule in either direction.
func TestCrawlSinceIgnoresImplausibleStamps(t *testing.T) {
	now := time.Now()
	epoch := -now.Sub(time.Unix(0, 0))
	fixture := &sinceFixture{now: now, pages: [][]time.Duration{
		{epoch, 48 * time.Hour, -1 * time.Hour}, // epoch + future + one real fresh stamp
		{epoch, -30 * time.Hour},                // boundary via the real stamp
	}}

	total := 0
	err := fixture.crawler().CrawlSince(t.Context(), now.Add(-24*time.Hour), func(posts []api.RawPost) error {
		total += len(posts)
		return nil
	})
	if err != nil {
		t.Fatalf("CrawlSince() error = %v", err)
	}
	if total != 5 {
		t.Fatalf("emitted %d posts, want all 5 including implausible ones", total)
	}
}

// A page whose stamps are all far older than the boundary is the STRONGEST
// possible stop signal, not an anomaly: plausibility must not be scaled to
// the settle window, or a short window turns those pages into "undatable" and
// fails every run.
func TestCrawlSinceStopsOnDefinitivelyOldPage(t *testing.T) {
	now := time.Now()
	fixture := &sinceFixture{now: now, pages: [][]time.Duration{
		repeatOffsets(5, -5*time.Minute),
		repeatOffsets(5, -30*24*time.Hour), // decades of settle windows behind
		repeatOffsets(5, -60*24*time.Hour),
	}}

	pages := 0
	err := fixture.crawler().CrawlSince(t.Context(), now.Add(-15*time.Minute), func([]api.RawPost) error {
		pages++
		return nil
	})
	if err != nil {
		t.Fatalf("CrawlSince() error = %v, want clean stop on the boundary page", err)
	}
	if pages != 2 {
		t.Fatalf("walked %d pages, want 2 (boundary page emitted, then stop)", pages)
	}
}

// The mirror image: a very wide settle window must not make a 1970 artifact
// plausible enough to stop the walk. Epoch pages stay undatable at any window
// width, so breakage still fails loudly instead of reading as "past the end".
func TestCrawlSinceRejectsEpochPagesUnderWideWindow(t *testing.T) {
	now := time.Now()
	epoch := -now.Sub(time.Unix(0, 0))
	fixture := &sinceFixture{now: now, pages: [][]time.Duration{
		{epoch, epoch},
		{epoch},
	}}

	err := fixture.crawler().CrawlSince(t.Context(), now.Add(-3650*24*time.Hour), func([]api.RawPost) error {
		return nil
	})
	if !errors.Is(err, ErrNoPlausibleDates) {
		t.Fatalf("CrawlSince() error = %v, want ErrNoPlausibleDates", err)
	}
}

// Consecutive content pages with zero plausible stamps indicate parser or
// markup breakage; the walk must fail loudly instead of walking the archive.
func TestCrawlSinceFailsOnConsecutiveUndatablePages(t *testing.T) {
	now := time.Now()
	epoch := -now.Sub(time.Unix(0, 0))
	fixture := &sinceFixture{now: now, pages: [][]time.Duration{
		{epoch, epoch},
		{epoch},
	}}

	pages := 0
	err := fixture.crawler().CrawlSince(t.Context(), now.Add(-24*time.Hour), func([]api.RawPost) error {
		pages++
		return nil
	})
	if !errors.Is(err, ErrNoPlausibleDates) {
		t.Fatalf("CrawlSince() error = %v, want ErrNoPlausibleDates", err)
	}
	if pages != 2 {
		t.Fatalf("emitted %d pages before failing, want 2 (posts still ingested)", pages)
	}
}

// The archive floor (consecutive empty pages) ends the walk cleanly when the
// listing runs out before the time bound is reached.
func TestCrawlSinceStopsAtArchiveFloor(t *testing.T) {
	now := time.Now()
	fixture := &sinceFixture{now: now, pages: [][]time.Duration{
		repeatOffsets(5, -1*time.Hour),
	}}

	pages := 0
	err := fixture.crawler().CrawlSince(t.Context(), now.Add(-240*time.Hour), func([]api.RawPost) error {
		pages++
		return nil
	})
	if err != nil {
		t.Fatalf("CrawlSince() error = %v", err)
	}
	if pages != 1 {
		t.Fatalf("emitted %d pages, want 1", pages)
	}
}

// An emit (ingest) failure ends the walk with an error; pages already emitted
// stay emitted — per-page progress is the point.
func TestCrawlSincePropagatesEmitError(t *testing.T) {
	now := time.Now()
	fixture := &sinceFixture{now: now, pages: [][]time.Duration{
		repeatOffsets(5, -1*time.Hour),
		repeatOffsets(5, -2*time.Hour),
	}}

	boom := errors.New("ingest down")
	pages := 0
	err := fixture.crawler().CrawlSince(t.Context(), now.Add(-240*time.Hour), func([]api.RawPost) error {
		pages++
		if pages == 2 {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("CrawlSince() error = %v, want wrapped emit error", err)
	}
	if pages != 2 {
		t.Fatalf("emit called %d times, want 2", pages)
	}
}

func TestPageFetchesOnePage(t *testing.T) {
	now := time.Now()
	fixture := &sinceFixture{now: now, pages: [][]time.Duration{
		repeatOffsets(3, -1*time.Hour),
		repeatOffsets(7, -2*time.Hour),
	}}

	posts, err := fixture.crawler().Page(t.Context(), 2)
	if err != nil {
		t.Fatalf("Page() error = %v", err)
	}
	if len(posts) != 7 {
		t.Fatalf("Page(2) = %d posts, want 7", len(posts))
	}

	empty, err := fixture.crawler().Page(t.Context(), 3)
	if err != nil {
		t.Fatalf("Page(3) error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("Page(3) = %d posts, want 0", len(empty))
	}
}

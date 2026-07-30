package crawl

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

func TestCrawlChunkResumesMidPageWithExactBudgets(t *testing.T) {
	pages := map[int][]api.RawPost{
		1: numberedPosts("p1", 80),
		2: numberedPosts("p2", 80),
		3: numberedPosts("p3", 80),
	}
	c := testChunkCrawler(pages, 2)

	first, err := c.CrawlChunk(t.Context(), 100, "", 0)
	if err != nil {
		t.Fatalf("first CrawlChunk() error = %v", err)
	}
	if len(first.Posts) != 100 || first.PagesFetched != 2 || !first.HasMore || first.StopReason != StopPageBudget {
		t.Fatalf("first = %+v", first)
	}
	page, offset, err := ParseCursor("test", first.NextCursor)
	if err != nil || page != 2 || offset != 20 {
		t.Fatalf("first cursor = (%d,%d,%v), want (2,20,nil)", page, offset, err)
	}
	if first.Posts[79].SourceID != "p1-79" || first.Posts[80].SourceID != "p2-0" ||
		first.Posts[99].SourceID != "p2-19" {
		t.Fatalf("first boundary IDs = %q, %q, %q",
			first.Posts[79].SourceID, first.Posts[80].SourceID, first.Posts[99].SourceID)
	}

	second, err := c.CrawlChunk(t.Context(), 100, first.NextCursor, 0)
	if err != nil {
		t.Fatalf("second CrawlChunk() error = %v", err)
	}
	if len(second.Posts) != 100 || second.PagesFetched != 2 || !second.HasMore {
		t.Fatalf("second = %+v", second)
	}
	page, offset, err = ParseCursor("test", second.NextCursor)
	if err != nil || page != 3 || offset != 40 {
		t.Fatalf("second cursor = (%d,%d,%v), want (3,40,nil)", page, offset, err)
	}
	if second.Posts[0].SourceID != "p2-20" || second.Posts[59].SourceID != "p2-79" ||
		second.Posts[60].SourceID != "p3-0" || second.Posts[99].SourceID != "p3-39" {
		t.Fatalf("second boundary IDs = %q, %q, %q, %q",
			second.Posts[0].SourceID, second.Posts[59].SourceID,
			second.Posts[60].SourceID, second.Posts[99].SourceID)
	}
}

func TestCrawlChunkResolvesEmptyRunBeforeParkingCursor(t *testing.T) {
	pages := map[int][]api.RawPost{
		1: {{SourceID: "one"}},
		2: {},
		3: {{SourceID: "two"}, {SourceID: "three"}},
	}
	c := testChunkCrawler(pages, 2)

	got, err := c.CrawlChunk(t.Context(), 2, "", 0)
	if err != nil {
		t.Fatalf("CrawlChunk() error = %v", err)
	}
	if got.PagesFetched != 3 || len(got.Posts) != 2 || got.Posts[1].SourceID != "two" {
		t.Fatalf("result = %+v", got)
	}
	page, offset, err := ParseCursor("test", got.NextCursor)
	if err != nil || page != 3 || offset != 1 {
		t.Fatalf("cursor = (%d,%d,%v), want (3,1,nil)", page, offset, err)
	}
}

func TestCrawlChunkReturnsPartialChunkAtConfirmedFloor(t *testing.T) {
	c := testChunkCrawler(map[int][]api.RawPost{
		1: {{SourceID: "one"}},
		2: {},
		3: {},
	}, 2)

	got, err := c.CrawlChunk(t.Context(), 2, "", 0)
	if err != nil {
		t.Fatalf("CrawlChunk() error = %v", err)
	}
	if len(got.Posts) != 1 || got.HasMore || got.NextCursor != "" ||
		got.StopReason != StopArchiveFloor || got.PagesFetched != 3 {
		t.Fatalf("result = %+v", got)
	}
}

func TestCrawlChunkConfirmsLookbackBoundaryAfterBudgetEdge(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	c := testChunkCrawlerWithNow(map[int][]api.RawPost{
		1: {
			{SourceID: "one", PublishedAt: now.Add(-time.Hour)},
			{SourceID: "two", PublishedAt: now.Add(-23 * time.Hour)},
			{SourceID: "old", PublishedAt: now.Add(-25 * time.Hour)},
		},
	}, 2, now)

	first, err := c.CrawlChunk(t.Context(), 2, "", 24*time.Hour)
	if err != nil {
		t.Fatalf("CrawlChunk() error = %v", err)
	}
	if len(first.Posts) != 2 || !first.HasMore || first.NextCursor == "" ||
		first.StopReason != StopPageBudget {
		t.Fatalf("first result = %+v", first)
	}

	second, err := c.CrawlChunk(t.Context(), 2, first.NextCursor, 24*time.Hour)
	if err != nil {
		t.Fatalf("resumed CrawlChunk() error = %v", err)
	}
	if len(second.Posts) != 0 || second.HasMore || second.NextCursor != "" ||
		second.StopReason != StopLookbackBoundary {
		t.Fatalf("second result = %+v", second)
	}
}

func TestCrawlChunkEpochArtifactDoesNotEndLookback(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	c := testChunkCrawlerWithNow(map[int][]api.RawPost{
		1: {
			{SourceID: "epoch", PublishedAt: time.Unix(0, 0).UTC()},
			{SourceID: "fresh", PublishedAt: now.Add(-time.Hour)},
		},
		2: {{SourceID: "old", PublishedAt: now.Add(-25 * time.Hour)}},
	}, 2, now)

	got, err := c.CrawlChunk(t.Context(), 10, "", 24*time.Hour)
	if err != nil {
		t.Fatalf("CrawlChunk() error = %v", err)
	}
	if got.HasMore || got.StopReason != StopLookbackBoundary {
		t.Fatalf("result = %+v", got)
	}
	if ids := sourceIDs(got.Posts); !reflect.DeepEqual(ids, []string{"epoch", "fresh"}) {
		t.Fatalf("posts = %v, want epoch artifact and fresh row", ids)
	}
}

func TestCrawlChunkPinnedOldRowDoesNotHideFreshRows(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	c := testChunkCrawlerWithNow(map[int][]api.RawPost{
		1: {
			{SourceID: "pinned-old", PublishedAt: now.Add(-30 * 24 * time.Hour)},
			{SourceID: "fresh-one", PublishedAt: now.Add(-time.Hour)},
			{SourceID: "fresh-two", PublishedAt: now.Add(-2 * time.Hour)},
		},
		2: {{SourceID: "old", PublishedAt: now.Add(-25 * time.Hour)}},
	}, 2, now)

	got, err := c.CrawlChunk(t.Context(), 10, "", 24*time.Hour)
	if err != nil {
		t.Fatalf("CrawlChunk() error = %v", err)
	}
	if got.HasMore || got.StopReason != StopLookbackBoundary {
		t.Fatalf("result = %+v", got)
	}
	if ids := sourceIDs(got.Posts); !reflect.DeepEqual(ids, []string{"fresh-one", "fresh-two"}) {
		t.Fatalf("posts = %v, want both fresh rows", ids)
	}
}

func TestCrawlChunkFailureReturnsNoResumeState(t *testing.T) {
	wantErr := errors.New("upstream failed")
	c := NewCrawler(Config{
		Source: "test",
		Fetch: func(_ context.Context, page int) ([]byte, error) {
			if page == 2 {
				return nil, wantErr
			}
			return []byte(strconv.Itoa(page)), nil
		},
		Parse: func(body []byte) ([]api.RawPost, error) {
			return []api.RawPost{{SourceID: string(body)}}, nil
		},
		Threshold: 2,
	})

	got, err := c.CrawlChunk(t.Context(), 2, "", 0)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if !reflect.DeepEqual(got, CrawlResponse{}) {
		t.Fatalf("response = %+v, want zero value", got)
	}
}

func TestParseCursorRejectsMalformedAndForeignCursors(t *testing.T) {
	tests := []struct {
		name   string
		source string
		cursor string
	}{
		{name: "not base64", source: "nyaa", cursor: "nope"},
		{name: "foreign source", source: "nyaa", cursor: FormatCursor("dmhy", 1, 0)},
		{name: "zero page", source: "nyaa", cursor: FormatCursor("nyaa", 0, 0)},
		{name: "negative offset", source: "nyaa", cursor: FormatCursor("nyaa", 1, -1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := ParseCursor(tt.source, tt.cursor); err == nil {
				t.Fatalf("ParseCursor(%q) accepted", tt.cursor)
			}
		})
	}
}

func testChunkCrawler(pages map[int][]api.RawPost, threshold int) *Crawler {
	return testChunkCrawlerWithNow(pages, threshold, time.Now())
}

func testChunkCrawlerWithNow(pages map[int][]api.RawPost, threshold int, now time.Time) *Crawler {
	return NewCrawler(Config{
		Source: "test",
		Fetch: func(_ context.Context, page int) ([]byte, error) {
			return []byte(strconv.Itoa(page)), nil
		},
		Parse: func(body []byte) ([]api.RawPost, error) {
			page, err := strconv.Atoi(string(body))
			if err != nil {
				return nil, fmt.Errorf("parse page: %w", err)
			}
			return pages[page], nil
		},
		Threshold: threshold,
		Now:       func() time.Time { return now },
	})
}

func numberedPosts(prefix string, count int) []api.RawPost {
	posts := make([]api.RawPost, count)
	for i := range posts {
		posts[i].SourceID = fmt.Sprintf("%s-%d", prefix, i)
	}
	return posts
}

func sourceIDs(posts []api.RawPost) []string {
	ids := make([]string, len(posts))
	for i := range posts {
		ids[i] = posts[i].SourceID
	}
	return ids
}

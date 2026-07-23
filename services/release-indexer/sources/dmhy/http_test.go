package dmhy

import (
	"context"
	"strings"
	"testing"
)

func TestArchivePageURLCategory(t *testing.T) {
	const base = "https://share.dmhy.org"
	cases := []struct {
		category, page int
		want           string
	}{
		{2, 5, "https://share.dmhy.org/topics/list/sort_id/2/page/5"},
		{31, 1, "https://share.dmhy.org/topics/list/sort_id/31/page/1"},
		{0, 5, "https://share.dmhy.org/topics/list/page/5"},
	}
	for _, c := range cases {
		if got := archivePageURL(base, c.category, c.page); got != c.want {
			t.Fatalf("archivePageURL(%q, %d, %d) = %q, want %q", base, c.category, c.page, got, c.want)
		}
	}
}

func TestHTTPCrawlerUsesCategory(t *testing.T) {
	crawler := NewHTTPCrawler("file:///nonexistent/dmhy-test", 31, 0, 0)

	_, err := crawler.Crawl(context.Background(), 1)
	if err == nil {
		t.Fatal("Crawl: want fetch error over missing file:// base, got nil")
	}
	if !strings.Contains(err.Error(), "sort_id/31") {
		t.Fatalf("Crawl error %q does not carry sort_id/31", err.Error())
	}
}

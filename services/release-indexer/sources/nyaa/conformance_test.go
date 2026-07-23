//go:build conformance

// Nyaa crawler conformance suite.
//
// Fixture provenance: testdata/live-listing-p2.html is a real Nyaa listing page
// fetched on 2026-07-04; testdata/live-no-results.html is a real empty-results
// page fetched the same day. Do not trim them.
package nyaa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/crawl"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/rawpost"
)

const (
	liveListingFixture   = "live-listing-p2.html"
	liveNoResultsFixture = "live-no-results.html"
)

var errNyaaTransient = errors.New("nyaa-conformance: injected transient fetch failure")

func loadTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestP4_NyaaParse_LiveListingGolden(t *testing.T) {
	posts, err := ParseListingPage(loadTestdata(t, liveListingFixture))
	if err != nil {
		t.Fatalf("ParseListingPage(%s): %v", liveListingFixture, err)
	}
	if len(posts) != 75 {
		t.Fatalf("len(posts) = %d, want 75", len(posts))
	}

	first := posts[0]
	if first.Source != rawpost.SourceNyaa {
		t.Fatalf("Source = %q, want %q", first.Source, rawpost.SourceNyaa)
	}
	if first.SourceID != "2128319" {
		t.Fatalf("SourceID = %q, want 2128319", first.SourceID)
	}
	if _, err := strconv.Atoi(first.SourceID); err != nil {
		t.Fatalf("SourceID = %q, want numeric: %v", first.SourceID, err)
	}
	if first.URL != "https://nyaa.si/view/2128319" {
		t.Fatalf("URL = %q, want https://nyaa.si/view/2128319", first.URL)
	}
	wantTitle := "[AsukaRaws] Mahou Shoujo Lyrical Nanoha EXGV - 01 (WEB-DL 1280x720 x264 AAC)"
	if first.Title != wantTitle {
		t.Fatalf("Title = %q, want %q", first.Title, wantTitle)
	}
	if !strings.HasPrefix(first.Magnet, "magnet:?xt=urn:btih:") {
		t.Fatalf("Magnet = %q, want btih magnet prefix", first.Magnet)
	}
	if first.SizeBytes != 584685978 {
		t.Fatalf("SizeBytes = %d, want 584685978", first.SizeBytes)
	}
	wantTime := time.Unix(1783185005, 0).UTC()
	if !first.PublishedAt.Equal(wantTime) {
		t.Fatalf("PublishedAt = %s, want %s", first.PublishedAt, wantTime)
	}
	if first.PublishedAt.Location() != time.UTC {
		t.Fatalf("PublishedAt location = %v, want UTC", first.PublishedAt.Location())
	}
}

func TestP4_NyaaParse_LiveNoResultsIsEmpty(t *testing.T) {
	posts, err := ParseListingPage(loadTestdata(t, liveNoResultsFixture))
	if err != nil {
		t.Fatalf("ParseListingPage(%s): %v", liveNoResultsFixture, err)
	}
	if len(posts) != 0 {
		t.Fatalf("len(posts) = %d, want 0", len(posts))
	}
}

func TestP4_NyaaCrawl_FloorRequiresConsecutiveEmptyPages(t *testing.T) {
	var fetched []int
	fetch := func(_ context.Context, page int) ([]byte, error) {
		fetched = append(fetched, page)
		switch page {
		case 1, 3, 4:
			return []byte(noResultsListingPage), nil
		case 2:
			return []byte(listingWithItems(200)), nil
		default:
			return nil, fmt.Errorf("unexpected page %d", page)
		}
	}

	posts, err := NewCrawler(fetch, Threshold).Crawl(context.Background(), 10)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if got := testPostIDs(posts); !reflect.DeepEqual(got, []string{"200"}) {
		t.Fatalf("post IDs = %v, want [200]", got)
	}
	if !reflect.DeepEqual(fetched, []int{1, 2, 3, 4}) {
		t.Fatalf("fetched = %v, want [1 2 3 4]", fetched)
	}
}

func TestP4_NyaaCrawl_ReturnsNewestBoundedBatch(t *testing.T) {
	fetch := func(_ context.Context, page int) ([]byte, error) {
		switch page {
		case 1:
			return []byte(listingWithItems(1, 2)), nil
		case 2:
			return []byte(listingWithItems(3, 4)), nil
		default:
			return []byte(noResultsListingPage), nil
		}
	}

	posts, err := NewCrawler(fetch, Threshold).Crawl(context.Background(), 3)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if got := testPostIDs(posts); !reflect.DeepEqual(got, []string{"1", "2", "3"}) {
		t.Fatalf("post IDs = %v, want [1 2 3]", got)
	}
}

func TestP4_NyaaCrawl_TransientFetchErrorReturnsNoPartialBatch(t *testing.T) {
	fetch := func(_ context.Context, page int) ([]byte, error) {
		if page == 1 {
			return []byte(listingWithItems(1)), nil
		}
		return nil, errNyaaTransient
	}

	posts, err := NewCrawler(fetch, Threshold).Crawl(context.Background(), 10)
	if !errors.Is(err, crawl.ErrCrawlFetch) || !errors.Is(err, errNyaaTransient) {
		t.Fatalf("Crawl error = %v, want wrapped transient fetch error", err)
	}
	if posts != nil {
		t.Fatalf("Crawl posts = %+v, want nil partial batch", posts)
	}
}

func TestP4_NyaaParse_BadSizeKeepsPostWithZeroBytes(t *testing.T) {
	body := strings.Replace(sampleListingPage, "1.4 GiB", "1 ZiB", 1)
	posts, err := ParseListingPage([]byte(body))
	if err != nil {
		t.Fatalf("ParseListingPage: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("len(posts) = %d, want 1", len(posts))
	}
	if posts[0].SizeBytes != 0 {
		t.Fatalf("SizeBytes = %d, want 0", posts[0].SizeBytes)
	}
}

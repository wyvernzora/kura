package nyaa

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/rawpost"
)

func TestCrawlReturnsNewestLimit(t *testing.T) {
	var fetched []int
	fetch := func(_ context.Context, page int) ([]byte, error) {
		fetched = append(fetched, page)
		switch page {
		case 1:
			return []byte(listingWithItems(1, 2)), nil
		case 2:
			return []byte(listingWithItems(3)), nil
		default:
			return []byte(emptyListingPage), nil
		}
	}
	c := NewCrawler(fetch, Threshold)

	posts, err := c.Crawl(context.Background(), 3)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if got := testPostIDs(posts); fmt.Sprint(got) != fmt.Sprint([]string{"1", "2", "3"}) {
		t.Fatalf("post IDs = %v, want [1 2 3]", got)
	}
	if fmt.Sprint(fetched) != fmt.Sprint([]int{1, 2}) {
		t.Fatalf("fetched pages = %v, want [1 2]", fetched)
	}
}

func testPostIDs(posts []rawpost.RawPost) []string {
	ids := make([]string, len(posts))
	for i, post := range posts {
		ids[i] = post.SourceID
	}
	return ids
}

func TestCrawlRequiresConsecutiveEmptyPagesForFeedFloor(t *testing.T) {
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
	c := NewCrawler(fetch, Threshold)

	posts, err := c.Crawl(context.Background(), 10)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if len(posts) != 1 || posts[0].SourceID != "200" {
		t.Fatalf("posts = %+v, want valid post after transient empty page", posts)
	}
	if fmt.Sprint(fetched) != fmt.Sprint([]int{1, 2, 3, 4}) {
		t.Fatalf("fetched pages = %v, want [1 2 3 4]", fetched)
	}
}

func listingWithItems(ids ...int) string {
	times := make([]time.Time, 0, len(ids))
	for range ids {
		times = append(times, time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC))
	}
	return listingWith(ids, times)
}

func listingWith(ids []int, times []time.Time) string {
	var rows string
	for i, id := range ids {
		rows += fmt.Sprintf(`<tr class="default">
<td><a href="/?c=1_2" title="Anime - English-translated"></a></td>
<td colspan="2"><a href="/view/%d" title="item %d">item %d</a></td>
<td class="text-center"><a href="/download/%d.torrent"></a><a href="magnet:?xt=urn:btih:%040d"></a></td>
<td class="text-center">1 MiB</td>
<td class="text-center" data-timestamp="%d">%s</td>
<td class="text-center">1</td><td class="text-center">0</td><td class="text-center">3</td>
</tr>`, id, id, id, id, id, times[i].Unix(), times[i].Format("2006-01-02 15:04"))
	}
	return `<table class="table torrent-list table-bordered"><tbody>` + rows + `</tbody></table>`
}

//go:build conformance

// DMHY parser and newest-window crawler conformance suite.
package dmhy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/crawl"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/rawpost"
)

const (
	htmlDir  = "testdata/html"
	floorPth = "testdata/floor.json"
)

var errTransient = errors.New("dmhy-conformance: injected transient fetch failure")

func loadHTMLBytes(t *testing.T, rel string) []byte {
	t.Helper()
	path := filepath.Join(htmlDir, rel)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: read fixture: %v", path, err)
	}
	return body
}

type floorArtifact struct {
	Sources struct {
		DMHY struct {
			Category31 struct {
				FloorPage     *int   `json:"floor_page"`
				CalendarFloor string `json:"calendar_floor"`
			} `json:"sort_id_31"`
		} `json:"dmhy"`
	} `json:"sources"`
	CrawlRateRPS              float64 `json:"crawl_rate_rps"`
	ConsecutiveEmptyThreshold int     `json:"consecutive_empty_threshold"`
}

func loadFloor(t *testing.T) floorArtifact {
	t.Helper()
	body, err := os.ReadFile(floorPth)
	if err != nil {
		t.Fatalf("%s: read floor artifact: %v", floorPth, err)
	}
	var artifact floorArtifact
	if err := json.Unmarshal(body, &artifact); err != nil {
		t.Fatalf("%s: decode floor artifact: %v", floorPth, err)
	}
	return artifact
}

func threshold(t *testing.T) int {
	t.Helper()
	n := loadFloor(t).ConsecutiveEmptyThreshold
	if n <= 0 {
		t.Fatalf("%s: consecutive_empty_threshold = %d, want > 0", floorPth, n)
	}
	return n
}

var contentRowRE = regexp.MustCompile(`<tr class="">`)

func pageRows(body []byte) int {
	return len(contentRowRE.FindAllString(string(body), -1))
}

func sequencePages(t *testing.T, sequence string, count int) [][]byte {
	t.Helper()
	dir := filepath.Join(htmlDir, sequence)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read sequence dir %s: %v", dir, err)
	}
	htmlCount := 0
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".html" {
			htmlCount++
		}
	}
	if htmlCount != count {
		t.Fatalf("%s: sequence has %d HTML files, want %d", dir, htmlCount, count)
	}
	pages := make([][]byte, count)
	for i := range count {
		pages[i] = loadHTMLBytes(t, filepath.Join(sequence, "page-"+strconv.Itoa(i+1)+".html"))
	}
	return pages
}

func sequenceFetcher(pages [][]byte, requested *[]int) PageFetcher {
	return func(_ context.Context, page int) ([]byte, error) {
		*requested = append(*requested, page)
		if page > len(pages) {
			page = len(pages)
		}
		return pages[page-1], nil
	}
}

type rowSpec struct {
	id   int
	date string
}

func buildArchivePage(rows []rowSpec) []byte {
	var body strings.Builder
	body.WriteString(`<html><body><table id="topic_list"><tbody><tr><th>header</th></tr>`)
	for _, row := range rows {
		fmt.Fprintf(&body, `<tr class=""><td width="98">`)
		if row.date != "" {
			fmt.Fprintf(&body, `<span style="display: none;">%s</span>`, row.date)
		}
		fmt.Fprintf(&body, `</td><td class="title"><a href="/topics/view/%d_synthetic.html" target="_blank">synthetic title %d</a></td>`, row.id, row.id)
		fmt.Fprintf(&body, `<td nowrap="nowrap" align="center"><a class="download-xl" data-magnet="magnet:?xt=urn:btih:%040x">x</a></td>`, row.id)
		body.WriteString(`<td nowrap="nowrap" align="center">3.6GB</td><td align="center"><a href="/topics/list/user_id/1">synth</a></td></tr>`)
	}
	body.WriteString(`</tbody></table></body></html>`)
	return []byte(body.String())
}

func syntheticRows(start, count int) []rowSpec {
	rows := make([]rowSpec, count)
	for i := range count {
		rows[i] = rowSpec{id: start + i, date: "2026/06/20 12:00"}
	}
	return rows
}

type scriptedFetcher struct {
	pages     [][]byte
	requested []int
}

func (s *scriptedFetcher) fetch(_ context.Context, page int) ([]byte, error) {
	s.requested = append(s.requested, page)
	if page > len(s.pages) {
		return buildArchivePage(nil), nil
	}
	return s.pages[page-1], nil
}

func postSourceIDs(posts []rawpost.RawPost) []string {
	ids := make([]string, len(posts))
	for i, post := range posts {
		ids[i] = post.SourceID
	}
	return ids
}

func TestP0_FixtureManifest(t *testing.T) {
	floor := loadFloor(t)
	n := floor.ConsecutiveEmptyThreshold
	if floor.Sources.DMHY.Category31.FloorPage == nil || *floor.Sources.DMHY.Category31.FloorPage <= 0 {
		t.Fatalf("%s: sort_id_31.floor_page must be positive", floorPth)
	}
	if n <= 0 || floor.CrawlRateRPS <= 0 {
		t.Fatalf("%s: invalid threshold or crawl rate: %+v", floorPth, floor)
	}

	realPage := loadHTMLBytes(t, "page-real.html")
	if pageRows(realPage) == 0 {
		t.Fatal("page-real.html has no content rows")
	}
	floorPage := loadHTMLBytes(t, "floor-empty.html")
	if pageRows(floorPage) != 0 || !strings.Contains(string(floorPage), `id="topic_list"`) {
		t.Fatal("floor-empty.html must be an empty archive page")
	}
	invalidPage := loadHTMLBytes(t, "non-archive-200.html")
	if pageRows(invalidPage) != 0 || strings.Contains(string(invalidPage), `id="topic_list"`) {
		t.Fatal("non-archive-200.html must lack the archive marker")
	}

	guard := sequencePages(t, "seq-guard", n)
	for i := 0; i < n-1; i++ {
		if pageRows(guard[i]) != 0 {
			t.Fatalf("seq-guard page %d is not empty", i+1)
		}
	}
	if pageRows(guard[n-1]) == 0 {
		t.Fatalf("seq-guard page %d must contain rows", n)
	}
	for i, page := range sequencePages(t, "seq-terminate", n) {
		if pageRows(page) != 0 {
			t.Fatalf("seq-terminate page %d is not empty", i+1)
		}
	}
}

func TestP1_CrawlerParsesArchivePage(t *testing.T) {
	posts, err := ParseArchivePage(loadHTMLBytes(t, "page-real.html"))
	if err != nil {
		t.Fatalf("ParseArchivePage: %v", err)
	}
	if len(posts) == 0 {
		t.Fatal("page-real.html emitted no posts")
	}
	for _, post := range posts {
		if post.SizeBytes > 0 && post.Source == rawpost.SourceDMHY &&
			post.SourceID != "" && post.Title != "" && strings.Contains(post.Magnet, "&tr=") {
			return
		}
	}
	t.Fatalf("no fully populated tracker-rich post: %+v", posts)
}

func TestP1_RowMagnetPrefersTrackerLink(t *testing.T) {
	row := `<a class="download-arrow arrow-magnet" href="magnet:?xt=urn:btih:BASE32HASH&dn=&tr=http%3A%2F%2Ftracker.example%2Fannounce">&nbsp;</a>` +
		`<a class="download-xl" data-magnet="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567">x</a>`
	if got := rowMagnet(row); !strings.Contains(got, "&tr=") {
		t.Fatalf("rowMagnet() = %q, want tracker-rich magnet", got)
	}
}

func TestP1_ParseSizeGolden(t *testing.T) {
	got, err := ParseSize("3.6GB")
	if err != nil {
		t.Fatalf("ParseSize: %v", err)
	}
	if got != 3_600_000_000 {
		t.Fatalf("ParseSize = %d, want 3600000000", got)
	}
}

func TestP4_Crawl_ConsecutiveEmptyThreshold(t *testing.T) {
	n := threshold(t)

	t.Run("guard reaches content", func(t *testing.T) {
		var requested []int
		posts, err := NewCrawler(sequenceFetcher(sequencePages(t, "seq-guard", n), &requested), n).
			Crawl(context.Background(), 1)
		if err != nil {
			t.Fatalf("Crawl: %v", err)
		}
		if len(posts) != 1 || !reflect.DeepEqual(requested, []int{1, 2}) {
			t.Fatalf("posts=%d requested=%v, want one post after pages [1 2]", len(posts), requested)
		}
	})

	t.Run("threshold confirms floor", func(t *testing.T) {
		var requested []int
		posts, err := NewCrawler(sequenceFetcher(sequencePages(t, "seq-terminate", n), &requested), n).
			Crawl(context.Background(), 10)
		if err != nil {
			t.Fatalf("Crawl: %v", err)
		}
		if len(posts) != 0 || len(requested) != n {
			t.Fatalf("posts=%d requested=%v, want confirmed floor after %d pages", len(posts), requested, n)
		}
	})
}

func TestP4_Crawl_EmptyRunContinuity(t *testing.T) {
	fetcher := &scriptedFetcher{pages: [][]byte{
		buildArchivePage(nil),
		buildArchivePage(syntheticRows(1, 2)),
		buildArchivePage(nil),
		buildArchivePage(nil),
	}}
	posts, err := NewCrawler(fetcher.fetch, threshold(t)).Crawl(context.Background(), 10)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if got := postSourceIDs(posts); !reflect.DeepEqual(got, []string{"1", "2"}) {
		t.Fatalf("post IDs = %v, want [1 2]", got)
	}
	if !reflect.DeepEqual(fetcher.requested, []int{1, 2, 3, 4}) {
		t.Fatalf("requested = %v, want [1 2 3 4]", fetcher.requested)
	}
}

func TestP4_Crawl_LimitBoundsNewestPosts(t *testing.T) {
	fetcher := &scriptedFetcher{pages: [][]byte{buildArchivePage(syntheticRows(1, 250))}}
	posts, err := NewCrawler(fetcher.fetch, threshold(t)).Crawl(context.Background(), 500)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if len(posts) != 200 || posts[0].SourceID != "1" || posts[199].SourceID != "200" {
		t.Fatalf("bounded posts = %d [%s..%s], want 200 [1..200]", len(posts), posts[0].SourceID, posts[len(posts)-1].SourceID)
	}
	if !reflect.DeepEqual(fetcher.requested, []int{1}) {
		t.Fatalf("requested = %v, want [1]", fetcher.requested)
	}
}

func TestP4_Crawl_ZeroLimitClampsToOne(t *testing.T) {
	posts, err := NewCrawler(
		(&scriptedFetcher{pages: [][]byte{buildArchivePage(syntheticRows(1, 2))}}).fetch,
		threshold(t),
	).Crawl(context.Background(), 0)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if len(posts) != 1 || posts[0].SourceID != "1" {
		t.Fatalf("posts = %+v, want first post only", posts)
	}
}

func TestP4_Crawl_TransientErrorReturnsNoPartialBatch(t *testing.T) {
	fetch := func(_ context.Context, page int) ([]byte, error) {
		if page == 1 {
			return buildArchivePage(syntheticRows(1, 1)), nil
		}
		return nil, errTransient
	}
	posts, err := NewCrawler(fetch, threshold(t)).Crawl(context.Background(), 10)
	if !errors.Is(err, crawl.ErrCrawlFetch) || !errors.Is(err, errTransient) {
		t.Fatalf("Crawl error = %v, want wrapped transient error", err)
	}
	if posts != nil {
		t.Fatalf("Crawl posts = %+v, want nil partial batch", posts)
	}
}

func TestP4_Crawl_NonArchivePageIsParseError(t *testing.T) {
	fetch := func(_ context.Context, _ int) ([]byte, error) {
		return loadHTMLBytes(t, "non-archive-200.html"), nil
	}
	posts, err := NewCrawler(fetch, threshold(t)).Crawl(context.Background(), 10)
	if !errors.Is(err, crawl.ErrCrawlParse) {
		t.Fatalf("Crawl error = %v, want parse error", err)
	}
	if posts != nil {
		t.Fatalf("Crawl posts = %+v, want nil", posts)
	}
}

package crawl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// This file is the resurrected stateless chunk engine from the standalone
// takuhai crawlers' POST /crawl: walk from an opaque cursor, consume exactly
// pageSize in-window posts, and park a cursor the CALLER threads into the
// next call. The engine holds no state across calls; the cursor is the state.

// Stop reasons reported on CrawlResponse.StopReason.
const (
	// StopPageBudget: the pageSize budget filled and more in-window posts
	// remain — thread NextCursor into the next call.
	StopPageBudget = "page_budget"
	// StopLookbackBoundary: the walk reached posts older than the lookback
	// window; the caller's loop is done.
	StopLookbackBoundary = "lookback_boundary"
	// StopArchiveFloor: the source's consecutive-empty floor was confirmed;
	// there is nothing further back.
	StopArchiveFloor = "archive_floor"
)

// CrawlResponse is one chunk's outcome: the consumed posts (newest → oldest)
// and the resume state. NextCursor is "" exactly when HasMore is false.
type CrawlResponse struct {
	Posts        []api.RawPost
	NextCursor   string
	HasMore      bool
	StopReason   string
	PagesFetched int
	LastPage     int
}

// Cursor is the resume point: the page last consumed and the count of its
// leading rows already returned. It travels as opaque base64url(JSON), the
// same shape the list-pagination cursor uses; Source binds it to the source
// that minted it so a cursor cannot silently resume a different listing.
type Cursor struct {
	Source string `json:"source"`
	Page   int    `json:"page"`
	Offset int    `json:"offset"`
}

// ParseCursor decodes an opaque cursor and validates its source binding.
// "" means nothing has been consumed yet (the walk starts at page 1). A
// malformed or foreign-source cursor is a hard error, never a silent restart.
func ParseCursor(source, encoded string) (page, offset int, err error) {
	if encoded == "" {
		return 0, 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return 0, 0, fmt.Errorf("%s: malformed cursor: %w", source, err)
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return 0, 0, fmt.Errorf("%s: malformed cursor: %w", source, err)
	}
	if c.Source != source {
		return 0, 0, fmt.Errorf("%s: cursor was minted for source %q", source, c.Source)
	}
	if c.Page < 1 || c.Offset < 0 {
		return 0, 0, fmt.Errorf("%s: malformed cursor: page must be positive and offset non-negative", source)
	}
	return c.Page, c.Offset, nil
}

// FormatCursor encodes the (page, offset) resume point as opaque
// base64url(JSON) bound to its source.
func FormatCursor(source string, page, offset int) string {
	raw, _ := json.Marshal(Cursor{Source: source, Page: page, Offset: offset})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// CrawlChunk runs one chunk: walk source pages from the cursor, consuming
// exactly pageSize in-window posts (newest → oldest), per the original
// crawler's algorithm:
//
//   - The cursor decodes to (page, offset): the page last consumed and the
//     count of its leading rows already returned. "" = (0, 0), so the first
//     fetch targets page 1. An offset > 0 cursor resumes ON its page (rows
//     remain — the short-TTL page cache absorbs the re-fetch); offset == 0
//     means that page is fully consumed, so the walk starts at page+1.
//   - lookback cutoff: a post older than now − lookback is out-of-window.
//     The walk is newest → oldest, so the first out-of-window post ends the
//     loop with HasMore=false. Posts with a zero/unparseable publishedAt are
//     KEPT in-window — a parse glitch must not truncate the walk. lookback
//     <= 0 means no limit.
//   - The consecutive-empty counter is per-walk, never persisted: a chunk
//     resolves any empty run it enters (content or the floor) before
//     parking a cursor — it never parks inside an unresolved empty run.
//   - HasMore is true when the budget fills without an observed boundary.
//     If the budget lands at a page boundary, the next call may discover
//     that the cursor was already at the floor. False means the lookback
//     boundary or floor was positively observed; NextCursor is then empty.
//   - ANY fetch/parse failure surfaces as a non-nil error and never looks
//     like the floor; the cursor is not advanced past the failed page, so a
//     retry re-fetches the same page and leaves no gap.
func (c *Crawler) CrawlChunk(ctx context.Context, pageSize int, cursor string, lookback time.Duration) (CrawlResponse, error) {
	curPage, curOffset, err := ParseCursor(c.source, cursor)
	if err != nil {
		return CrawlResponse{}, err
	}

	pageSize = clampLimit(pageSize)

	// cutoff is the lookback boundary; zero lookback means no row is ever
	// out-of-window. Computed once off the injectable clock.
	var cutoff time.Time
	hasCutoff := lookback > 0
	if hasCutoff {
		cutoff = c.now().Add(-lookback)
	}

	var (
		posts            []api.RawPost
		consecutiveEmpty int
		// page is the page to fetch next; skip is the leading rows to drop
		// on the FIRST fetched page.
		page         = curPage
		skip         = curOffset
		pagesFetched int
		lastPage     int
	)
	if skip == 0 {
		page++
	}

	for {
		pagePosts, err := c.Page(ctx, page)
		pagesFetched++
		lastPage = page
		if err != nil {
			// Transient failures surface verbatim and never advance the
			// cursor past the failed page.
			return CrawlResponse{}, err
		}

		if len(pagePosts) == 0 {
			// Positively-confirmed empty page (fetched OK, zero parsed
			// rows). Resolve the empty run before returning anything.
			consecutiveEmpty++
			if consecutiveEmpty >= c.threshold {
				return chunkResponse(posts, "", false, StopArchiveFloor, pagesFetched, lastPage), nil
			}
			page++
			skip = 0
			continue
		}
		consecutiveEmpty = 0

		// On the first fetched page of THIS call, drop the leading rows the
		// cursor already returned.
		pageStart := skip
		skip = 0
		if pageStart > len(pagePosts) {
			pageStart = len(pagePosts)
		}

		for i := pageStart; i < len(pagePosts); i++ {
			p := pagePosts[i]
			if outOfWindow(p.PublishedAt, cutoff, hasCutoff) {
				// Newest → oldest: the first out-of-window post means all
				// following are too.
				return chunkResponse(posts, "", false, StopLookbackBoundary, pagesFetched, lastPage), nil
			}
			posts = append(posts, p)

			if len(posts) < pageSize {
				continue
			}

			// Budget filled. Resolve HasMore against rows already in hand
			// before parking a cursor.
			if i+1 < len(pagePosts) {
				if outOfWindow(pagePosts[i+1].PublishedAt, cutoff, hasCutoff) {
					// The boundary lands exactly at the budget edge.
					return chunkResponse(posts, "", false, StopLookbackBoundary, pagesFetched, lastPage), nil
				}
				// Resume mid-page at offset i+1: the count of this page's
				// rows now returned.
				return chunkResponse(posts, FormatCursor(c.source, page, i+1), true, StopPageBudget, pagesFetched, lastPage), nil
			}
			// Budget filled exactly at the page's last row. Park (page, 0):
			// decode treats offset==0 as "page fully consumed → page+1",
			// which resumes by fetching the next page without dropping any
			// of its rows.
			return chunkResponse(posts, FormatCursor(c.source, page, 0), true, StopPageBudget, pagesFetched, lastPage), nil
		}

		// Page exhausted without filling the budget — walk deeper.
		page++
	}
}

func chunkResponse(posts []api.RawPost, nextCursor string, hasMore bool, stopReason string, pagesFetched, lastPage int) CrawlResponse {
	return CrawlResponse{
		Posts:        posts,
		NextCursor:   nextCursor,
		HasMore:      hasMore,
		StopReason:   stopReason,
		PagesFetched: pagesFetched,
		LastPage:     lastPage,
	}
}

// outOfWindow reports whether a post's publishedAt falls before the lookback
// cutoff. A zero timestamp is KEPT in-window: a parse glitch must not
// truncate the walk.
func outOfWindow(publishedAt, cutoff time.Time, hasCutoff bool) bool {
	if !hasCutoff {
		return false
	}
	return !publishedAt.IsZero() && publishedAt.Before(cutoff)
}

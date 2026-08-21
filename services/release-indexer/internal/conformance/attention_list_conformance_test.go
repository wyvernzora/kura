//go:build conformance

package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/internal/cursor"
	"github.com/wyvernzora/kura/services/release-indexer/internal/dispatch"
	"github.com/wyvernzora/kura/services/release-indexer/internal/rest"
	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// Four distinct infohashes for the multi-row list scenarios.
const (
	listIH1 = "1111111111111111111111111111111111111111"
	listIH2 = "2222222222222222222222222222222222222222"
	listIH3 = "3333333333333333333333333333333333333333"
	listIH4 = "4444444444444444444444444444444444444444"
)

// matchWith drives a fresh release to matched with an explicit ref and score,
// which is what the confidence filter is exercised against.
func matchWith(t *testing.T, ctx context.Context, st store.Store, clock *fakeClock, ih, sourceID, ref string, confidence *float64) {
	t.Helper()
	seedRelease(t, ctx, st, ih, sourceID, clock.now)
	claimed := claimTheRelease(t, ctx, st, ih)
	if err := st.Submit(ctx, store.SubmitParams{
		Infohash:   claimed.Infohash,
		ClaimToken: claimed.ClaimToken,
		Status:     "matched",
		Ref:        ref,
		Confidence: confidence,
	}); err != nil {
		t.Fatalf("Submit matched %s: %v", ih, err)
	}
}

// suppressOne drives a fresh release to suppressed the only way there is: a
// claim-fenced matcher submit.
func suppressOne(t *testing.T, ctx context.Context, st store.Store, clock *fakeClock, ih, sourceID string) {
	t.Helper()
	seedRelease(t, ctx, st, ih, sourceID, clock.now)
	claimed := claimTheRelease(t, ctx, st, ih)
	if err := st.Submit(ctx, store.SubmitParams{
		Infohash:   claimed.Infohash,
		ClaimToken: claimed.ClaimToken,
		Status:     "suppressed",
		Confidence: ptr(0.2),
		Reason:     "not wanted",
	}); err != nil {
		t.Fatalf("Submit suppressed %s: %v", ih, err)
	}
}

// exhaustOne burns a release's whole attempt budget, which is how a release
// reaches the state the attention surface exists for.
func exhaustOne(t *testing.T, ctx context.Context, st store.Store, clock *fakeClock, ih, sourceID string) {
	t.Helper()
	seedRelease(t, ctx, st, ih, sourceID, clock.now)
	for i := 0; i < 3; i++ {
		claimed := claimTheRelease(t, ctx, st, ih)
		if err := st.Submit(ctx, store.SubmitParams{
			Infohash:   ih,
			ClaimToken: claimed.ClaimToken,
			Status:     "unmatched",
			Reason:     "no candidate",
		}); err != nil {
			t.Fatalf("Submit unmatched %s attempt %d: %v", ih, i+1, err)
		}
		clock.Advance(61 * time.Second)
	}
	detail, err := st.GetRelease(ctx, ih)
	if err != nil {
		t.Fatalf("GetRelease %s: %v", ih, err)
	}
	if detail.MatchStatus != "exhausted" {
		t.Fatalf("fixture left %s in %q, want exhausted", ih, detail.MatchStatus)
	}
}

// claimTheRelease claims one item and fails loudly when the queue handed back a
// different release than the fixture is building, so a scenario can never end up
// asserting against a row it did not mean to touch.
func claimTheRelease(t *testing.T, ctx context.Context, st store.Store, ih string) store.ClaimedRelease {
	t.Helper()
	claimed := claimOne(t, ctx, st, 60)
	if claimed.Infohash != ih {
		t.Fatalf("claim returned %s, want %s — the fixture is driving the wrong release", claimed.Infohash, ih)
	}
	return claimed
}

func listReleases(t *testing.T, ctx context.Context, st store.Store, req dispatch.ListReleasesRequest) api.ReleaseList {
	t.Helper()
	out, err := dispatch.New(st).ListReleasesTyped(ctx, req)
	if err != nil {
		t.Fatalf("ListReleases %+v: %v", req, err)
	}
	return out
}

func listedInfohashes(list api.ReleaseList) []string {
	out := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		out = append(out, item.Infohash)
	}
	return out
}

// The unfiltered list is the pipeline's contract and stays matched-only. n8n and
// the CLI read it with no status parameter and must not start seeing the queue.
func TestAttentionList_DefaultStaysMatchedOnly(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)

	matchWith(t, ctx, st, clock, listIH1, "attn-matched", "tvdb:123", ptr(0.94))
	clock.Advance(time.Minute)
	suppressOne(t, ctx, st, clock, listIH2, "attn-suppressed")
	clock.Advance(time.Minute)
	exhaustOne(t, ctx, st, clock, listIH3, "attn-exhausted")

	// Pre-condition: the non-matched rows really are in the store, so the
	// assertion below is about the filter and not about an empty database.
	qs, err := st.QueueStats(ctx)
	if err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if qs.Suppressed != 1 || qs.Exhausted != 1 || qs.Matched != 1 {
		t.Fatalf("QueueStats = %+v, want one row in each of matched/suppressed/exhausted", qs)
	}

	got := listReleases(t, ctx, st, dispatch.ListReleasesRequest{})
	if len(got.Items) != 1 || got.Items[0].Infohash != listIH1 {
		t.Fatalf("unfiltered list = %v, want only the matched release %s", listedInfohashes(got), listIH1)
	}
	if got.Items[0].MatchStatus != api.MatchStatusMatched {
		t.Fatalf("matchStatus = %q, want matched on every row", got.Items[0].MatchStatus)
	}
}

// The attention set is one query: the statuses the operator can act on, newest
// first, with the status on each row so the list renders without a second call.
func TestAttentionList_StatusFilterReturnsTheAttentionSet(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)

	matchWith(t, ctx, st, clock, listIH1, "attn2-matched", "tvdb:123", ptr(0.94))
	clock.Advance(time.Hour)
	suppressOne(t, ctx, st, clock, listIH2, "attn2-suppressed")
	clock.Advance(time.Hour)
	exhaustOne(t, ctx, st, clock, listIH3, "attn2-exhausted")

	got := listReleases(t, ctx, st, dispatch.ListReleasesRequest{
		Statuses: []string{"exhausted", "suppressed"},
	})
	// Newest published first: the exhausted release was seeded an hour after
	// the suppressed one.
	want := []string{listIH3, listIH2}
	if gotIHs := listedInfohashes(got); !slices.Equal(gotIHs, want) {
		t.Fatalf("filtered list = %v, want %v newest first", gotIHs, want)
	}
	if got.Items[0].MatchStatus != api.MatchStatusExhausted || got.Items[1].MatchStatus != api.MatchStatusSuppressed {
		t.Fatalf("statuses = %q, %q; want exhausted then suppressed",
			got.Items[0].MatchStatus, got.Items[1].MatchStatus)
	}
	// A ref filter still composes: the exhausted/suppressed rows carry none,
	// so narrowing to the matched release's ref empties the page.
	narrowed := listReleases(t, ctx, st, dispatch.ListReleasesRequest{
		Statuses: []string{"exhausted", "suppressed"},
		Ref:      "tvdb:123",
	})
	if len(narrowed.Items) != 0 {
		t.Fatalf("ref-narrowed attention list = %v, want empty", listedInfohashes(narrowed))
	}
}

// maxConfidence is caller-supplied policy. A strict `<` keeps unscored matches
// out: no score is not a low score.
func TestAttentionList_MaxConfidenceSelectsLowScoringMatchesOnly(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)

	matchWith(t, ctx, st, clock, listIH1, "conf-low", "tvdb:1", ptr(0.51))
	clock.Advance(time.Hour)
	matchWith(t, ctx, st, clock, listIH2, "conf-high", "tvdb:2", ptr(0.94))
	clock.Advance(time.Hour)
	matchWith(t, ctx, st, clock, listIH3, "conf-none", "tvdb:3", nil)
	clock.Advance(time.Hour)
	matchWith(t, ctx, st, clock, listIH4, "conf-at-ceiling", "tvdb:4", ptr(0.75))

	// Pre-condition: all four are listed without the ceiling, so the filter
	// below is doing the work rather than an empty result faking it.
	all := listReleases(t, ctx, st, dispatch.ListReleasesRequest{})
	if len(all.Items) != 4 {
		t.Fatalf("unfiltered list returned %d items, want 4 before filtering", len(all.Items))
	}

	got := listReleases(t, ctx, st, dispatch.ListReleasesRequest{MaxConfidence: ptr(0.75)})
	if gotIHs := listedInfohashes(got); !slices.Equal(gotIHs, []string{listIH1}) {
		t.Fatalf("maxConfidence=0.75 list = %v, want only the 0.51 match %s "+
			"(0.94 too high, 0.75 not strictly below, unscored excluded)", gotIHs, listIH1)
	}
}

// A cursor is bound to the filters it was issued under. Replaying it against a
// different filter would seek with a key from a scan the caller is not running.
func TestAttentionList_CursorBindsTheStatusAndConfidenceFilters(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)

	suppressOne(t, ctx, st, clock, listIH1, "cur-suppressed")
	clock.Advance(time.Hour)
	exhaustOne(t, ctx, st, clock, listIH2, "cur-exhausted")

	filter := dispatch.ListReleasesRequest{Statuses: []string{"exhausted", "suppressed"}, Limit: 1}
	first := listReleases(t, ctx, st, filter)
	if len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatalf("first page = %v (cursor %v), want one row and a next cursor",
			listedInfohashes(first), first.NextCursor)
	}

	// Same filters: the cursor resumes.
	resume := filter
	resume.Cursor = *first.NextCursor
	second := listReleases(t, ctx, st, resume)
	if len(second.Items) != 1 || second.Items[0].Infohash == first.Items[0].Infohash {
		t.Fatalf("second page = %v, want the other row", listedInfohashes(second))
	}

	// Tampered filters: rejected rather than silently paging something else.
	for name, tampered := range map[string]dispatch.ListReleasesRequest{
		"narrowed status set": {Statuses: []string{"exhausted"}, Limit: 1, Cursor: *first.NextCursor},
		"dropped status set":  {Limit: 1, Cursor: *first.NextCursor},
		"added confidence":    {Statuses: []string{"exhausted", "suppressed"}, MaxConfidence: ptr(0.75), Limit: 1, Cursor: *first.NextCursor},
		"different ref":       {Statuses: []string{"exhausted", "suppressed"}, Ref: "tvdb:999", Limit: 1, Cursor: *first.NextCursor},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := dispatch.New(st).ListReleasesTyped(ctx, tampered)
			if !errors.Is(err, cursor.ErrInvalidCursor) {
				t.Fatalf("err = %v, want ErrInvalidCursor", err)
			}
		})
	}
}

// The same filters over the real HTTP surface, including the codes a client
// switches on.
func TestAttentionList_RESTFilterContract(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)
	handler := rest.New(st)

	matchWith(t, ctx, st, clock, listIH1, "rest-matched", "tvdb:123", ptr(0.4))
	clock.Advance(time.Hour)
	exhaustOne(t, ctx, st, clock, listIH2, "rest-exhausted")

	t.Run("status filter returns the exhausted row", func(t *testing.T) {
		body := getList(t, handler, "/api/releases/v1?status=exhausted")
		if len(body.Items) != 1 || body.Items[0].Infohash != listIH2 {
			t.Fatalf("items = %v, want only %s", listedInfohashes(body), listIH2)
		}
		if body.Items[0].MatchStatus != api.MatchStatusExhausted {
			t.Fatalf("matchStatus = %q, want exhausted", body.Items[0].MatchStatus)
		}
	})

	t.Run("confidence ceiling returns the low match", func(t *testing.T) {
		body := getList(t, handler, "/api/releases/v1?maxConfidence=0.75")
		if len(body.Items) != 1 || body.Items[0].Infohash != listIH1 {
			t.Fatalf("items = %v, want only %s", listedInfohashes(body), listIH1)
		}
	})

	t.Run("default is still matched only", func(t *testing.T) {
		body := getList(t, handler, "/api/releases/v1")
		if len(body.Items) != 1 || body.Items[0].Infohash != listIH1 {
			t.Fatalf("items = %v, want only the matched release %s", listedInfohashes(body), listIH1)
		}
	})

	for name, path := range map[string]string{
		"unknown status":     "/api/releases/v1?status=defer",
		"status with since":  "/api/releases/v1?status=exhausted&since=2026-06-24T12:00:00Z",
		"ceiling above one":  "/api/releases/v1?maxConfidence=2",
		"unparseable number": "/api/releases/v1?maxConfidence=nope",
	} {
		t.Run(name+" is rejected", func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody).WithContext(ctx))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s = %d, want 400; response %s", path, rec.Code, rec.Body.String())
			}
			var body api.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Kind != api.KindInvalidRequest {
				t.Fatalf("kind = %q, want %q", body.Kind, api.KindInvalidRequest)
			}
		})
	}

	t.Run("a tampered cursor is invalid_cursor", func(t *testing.T) {
		page := getList(t, handler, "/api/releases/v1?status=exhausted,matched&limit=1")
		if page.NextCursor == nil {
			t.Fatal("no next cursor on a two-row filtered page of one")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/api/releases/v1?status=exhausted&limit=1&cursor="+*page.NextCursor, http.NoBody).WithContext(ctx))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("replayed cursor = %d, want 400; response %s", rec.Code, rec.Body.String())
		}
		var body api.Error
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Kind != api.KindInvalidCursor {
			t.Fatalf("kind = %q, want %q", body.Kind, api.KindInvalidCursor)
		}
	})
}

func getList(t *testing.T, handler http.Handler, path string) api.ReleaseList {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200; response %s", path, rec.Code, rec.Body.String())
	}
	var body api.ReleaseList
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return body
}

//go:build conformance

package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/internal/cursor"
	"github.com/wyvernzora/kura/services/release-indexer/internal/rest"
	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

const (
	opIH1 = "aaaa111111111111111111111111111111111111"
	opIH2 = "bbbb222222222222222222222222222222222222"
	opIH3 = "cccc333333333333333333333333333333333333"
)

func mustGetRelease(t *testing.T, ctx context.Context, st store.Store, ih string) store.ReleaseDetail {
	t.Helper()
	detail, err := st.GetRelease(ctx, ih)
	if err != nil {
		t.Fatalf("GetRelease %s: %v", ih, err)
	}
	return detail
}

func lastMatchEvent(t *testing.T, detail store.ReleaseDetail) store.MatchEventDetail {
	t.Helper()
	if len(detail.MatchEvents) == 0 {
		t.Fatal("no match_events recorded")
	}
	return detail.MatchEvents[len(detail.MatchEvents)-1]
}

// assertOperatorMatch is the shared shape of every hand match: the ref the
// operator chose, full confidence, and an audit row saying so.
func assertOperatorMatch(t *testing.T, detail store.ReleaseDetail, wantRef, wantReason string) {
	t.Helper()
	if detail.MatchStatus != "matched" {
		t.Fatalf("MatchStatus = %q, want matched", detail.MatchStatus)
	}
	if detail.Ref == nil || *detail.Ref != wantRef {
		t.Fatalf("Ref = %v, want %q", detail.Ref, wantRef)
	}
	// An operator deciding by hand is certain by definition, which is also
	// what lifts the release out of a low-confidence attention list.
	if detail.Confidence == nil || *detail.Confidence != 1.0 {
		t.Fatalf("Confidence = %v, want 1.0 for an operator match", detail.Confidence)
	}
	if detail.FirstMatchedAt == nil {
		t.Fatal("FirstMatchedAt = nil, want the hand match stamped")
	}
	last := lastMatchEvent(t, detail)
	if last.Status != "matched" {
		t.Fatalf("last match_event status = %q, want matched", last.Status)
	}
	if last.Ref == nil || *last.Ref != wantRef {
		t.Fatalf("last match_event ref = %v, want %q", last.Ref, wantRef)
	}
	if last.Confidence == nil || *last.Confidence != 1.0 {
		t.Fatalf("last match_event confidence = %v, want 1.0", last.Confidence)
	}
	if wantReason != "" && (last.Reason == nil || *last.Reason != wantReason) {
		t.Fatalf("last match_event reason = %v, want %q", last.Reason, wantReason)
	}
}

// A suppressed release has no way out but a hand match: there is no unsuppress.
func TestOperatorStatus_HandMatchFromSuppressed(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)
	suppressOne(t, ctx, st, clock, opIH1, "op-suppressed")

	// Pre-condition: the submit left it suppressed with no ref, so the
	// assertions below cannot be satisfied by the fixture alone.
	before := mustGetRelease(t, ctx, st, opIH1)
	if before.MatchStatus != "suppressed" || before.Ref != nil {
		t.Fatalf("fixture = status %q ref %v, want suppressed with no ref", before.MatchStatus, before.Ref)
	}

	clock.Advance(time.Minute)
	if err := st.SetStatus(ctx, store.SetStatusParams{
		Infohash: opIH1,
		Status:   "matched",
		Ref:      "tvdb:370070",
		Reason:   "matched by hand",
	}); err != nil {
		t.Fatalf("SetStatus suppressed -> matched: %v", err)
	}
	assertOperatorMatch(t, mustGetRelease(t, ctx, st, opIH1), "tvdb:370070", "matched by hand")
}

// The exhausted case is the one the attention list is built around, and the
// magnet gate is what proves the release rejoined the pipeline.
func TestOperatorStatus_HandMatchFromExhaustedOpensTheMagnetGate(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)
	exhaustOne(t, ctx, st, clock, opIH1, "op-exhausted")

	// Pre-condition: an exhausted release resolves no magnet, so the check
	// after the hand match is not vacuously true.
	magnets, err := st.ResolveMagnets(ctx, []string{opIH1})
	if err != nil {
		t.Fatalf("ResolveMagnets while exhausted: %v", err)
	}
	if _, ok := magnets[opIH1]; ok {
		t.Fatal("magnet resolved while exhausted; the gate assertion would prove nothing")
	}

	if err := st.SetStatus(ctx, store.SetStatusParams{
		Infohash: opIH1,
		Status:   "matched",
		Ref:      "tvdb:370070",
	}); err != nil {
		t.Fatalf("SetStatus exhausted -> matched: %v", err)
	}
	assertOperatorMatch(t, mustGetRelease(t, ctx, st, opIH1), "tvdb:370070", "")

	magnets, err = st.ResolveMagnets(ctx, []string{opIH1})
	if err != nil {
		t.Fatalf("ResolveMagnets after the hand match: %v", err)
	}
	if _, ok := magnets[opIH1]; !ok {
		t.Fatal("magnet still gated after an operator match, want it downloadable")
	}
}

// Affirm and correct are the same transition with different refs: matched ->
// matched. Affirming lifts the score to 1.0; correcting also replaces the ref.
func TestOperatorStatus_AffirmAndCorrectAnExistingMatch(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)
	matchWith(t, ctx, st, clock, opIH1, "op-low", "tvdb:111", ptr(0.42))

	before := mustGetRelease(t, ctx, st, opIH1)
	if before.Confidence == nil || *before.Confidence != 0.42 {
		t.Fatalf("fixture confidence = %v, want the matcher's 0.42", before.Confidence)
	}

	// Affirm: same ref, now certain.
	clock.Advance(time.Minute)
	if err := st.SetStatus(ctx, store.SetStatusParams{
		Infohash: opIH1, Status: "matched", Ref: "tvdb:111", Reason: "affirmed",
	}); err != nil {
		t.Fatalf("affirm matched -> matched: %v", err)
	}
	affirmed := mustGetRelease(t, ctx, st, opIH1)
	assertOperatorMatch(t, affirmed, "tvdb:111", "affirmed")
	if len(affirmed.MatchEvents) != len(before.MatchEvents)+1 {
		t.Fatalf("match_events = %d, want exactly one row appended for the affirm",
			len(affirmed.MatchEvents))
	}

	// Repeating the affirm changes nothing and appends nothing: same target,
	// same ref.
	clock.Advance(time.Minute)
	if err := st.SetStatus(ctx, store.SetStatusParams{
		Infohash: opIH1, Status: "matched", Ref: "tvdb:111", Reason: "affirmed again",
	}); err != nil {
		t.Fatalf("repeat affirm: %v", err)
	}
	repeated := mustGetRelease(t, ctx, st, opIH1)
	if len(repeated.MatchEvents) != len(affirmed.MatchEvents) {
		t.Fatalf("match_events grew from %d to %d on a repeat with the same ref",
			len(affirmed.MatchEvents), len(repeated.MatchEvents))
	}

	// Correct: a different ref is a new decision, not a retry.
	clock.Advance(time.Minute)
	if err := st.SetStatus(ctx, store.SetStatusParams{
		Infohash: opIH1, Status: "matched", Ref: "tvdb:222", Reason: "wrong series",
	}); err != nil {
		t.Fatalf("correct matched -> matched: %v", err)
	}
	corrected := mustGetRelease(t, ctx, st, opIH1)
	assertOperatorMatch(t, corrected, "tvdb:222", "wrong series")
	if len(corrected.MatchEvents) != len(repeated.MatchEvents)+1 {
		t.Fatalf("match_events = %d, want one row appended for the correction",
			len(corrected.MatchEvents))
	}
	// first_matched_at is preserved: the release was first matched by the
	// matcher, and the correction does not rewrite when that happened.
	if corrected.FirstMatchedAt == nil || !corrected.FirstMatchedAt.Equal(*before.FirstMatchedAt) {
		t.Fatalf("FirstMatchedAt = %v, want the original %v preserved",
			corrected.FirstMatchedAt, before.FirstMatchedAt)
	}
}

// Requeue puts an exhausted release back in front of the matcher. Resetting the
// exhaustion accounting IS the requeue: leaving attempt_count at the cap would
// exhaust the release again on the next claim sweep without one new attempt.
func TestOperatorStatus_RequeueMakesAnExhaustedReleaseClaimableAgain(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)
	exhaustOne(t, ctx, st, clock, opIH1, "op-requeue")

	before := mustGetRelease(t, ctx, st, opIH1)
	if before.AttemptCount != 3 {
		t.Fatalf("fixture attempt_count = %d, want the cap of 3", before.AttemptCount)
	}
	// Pre-condition: an exhausted release is offered to nobody.
	empty, err := st.Claim(ctx, store.ClaimParams{LeaseSeconds: 60})
	if err != nil {
		t.Fatalf("Claim while exhausted: %v", err)
	}
	if len(empty.Items) != 0 {
		t.Fatalf("claim returned %d items while exhausted, want none", len(empty.Items))
	}

	clock.Advance(time.Minute)
	if err := st.SetStatus(ctx, store.SetStatusParams{
		Infohash: opIH1, Status: "unmatched", Reason: "new source posting",
	}); err != nil {
		t.Fatalf("SetStatus exhausted -> unmatched: %v", err)
	}

	after := mustGetRelease(t, ctx, st, opIH1)
	if after.MatchStatus != "unmatched" {
		t.Fatalf("MatchStatus = %q, want unmatched", after.MatchStatus)
	}
	if after.AttemptCount != 0 {
		t.Fatalf("AttemptCount = %d, want 0 — a requeue that keeps the count re-exhausts immediately", after.AttemptCount)
	}
	last := lastMatchEvent(t, after)
	if last.Status != "unmatched" || last.Ref != nil || last.Confidence != nil {
		t.Fatalf("last match_event = %+v, want an unmatched row with no ref or score", last)
	}
	if last.Reason == nil || *last.Reason != "new source posting" {
		t.Fatalf("last match_event reason = %v, want the submitted reason", last.Reason)
	}

	qs, err := st.QueueStats(ctx)
	if err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if qs.Available != 1 || qs.Exhausted != 0 {
		t.Fatalf("QueueStats = %+v, want available=1 exhausted=0", qs)
	}
	// The point of the whole transition: the matcher gets it back.
	reclaimed := claimTheRelease(t, ctx, st, opIH1)
	if reclaimed.AttemptCount != 0 {
		t.Fatalf("re-claimed with attempt_count = %d, want 0", reclaimed.AttemptCount)
	}

	// A repeat requeue of an already-unmatched release is an idempotent no-op.
	clock.Advance(time.Minute)
	if err := st.SetStatus(ctx, store.SetStatusParams{Infohash: opIH1, Status: "unmatched"}); err != nil {
		t.Fatalf("repeat requeue: %v", err)
	}
	repeated := mustGetRelease(t, ctx, st, opIH1)
	if len(repeated.MatchEvents) != len(after.MatchEvents) {
		t.Fatalf("match_events grew from %d to %d on a repeat requeue",
			len(after.MatchEvents), len(repeated.MatchEvents))
	}
}

// The ref rules are per target: matched is defined by its ref, everything else
// takes none. They are checked after the transition table, so a transition that
// was never on offer is still reported as a conflict.
func TestOperatorStatus_RefRules(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)

	suppressOne(t, ctx, st, clock, opIH1, "op-refrules-suppressed")
	exhaustOne(t, ctx, st, clock, opIH2, "op-refrules-exhausted")
	matchWith(t, ctx, st, clock, opIH3, "op-refrules-matched", "tvdb:1", ptr(0.9))

	for _, tt := range []struct {
		name string
		p    store.SetStatusParams
		want error
	}{
		{
			name: "hand match without a ref",
			p:    store.SetStatusParams{Infohash: opIH1, Status: "matched"},
			want: store.ErrRefRequired,
		},
		{
			name: "hand match with a malformed ref",
			p:    store.SetStatusParams{Infohash: opIH1, Status: "matched", Ref: "not-a-ref"},
			want: cursor.ErrInvalidRef,
		},
		{
			name: "requeue carrying a ref",
			p:    store.SetStatusParams{Infohash: opIH2, Status: "unmatched", Ref: "tvdb:1"},
			want: store.ErrRefForbidden,
		},
		{
			name: "dead carrying a ref",
			p:    store.SetStatusParams{Infohash: opIH3, Status: "dead", Ref: "tvdb:1"},
			want: store.ErrRefForbidden,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := st.SetStatus(ctx, tt.p); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			// A rejected call writes nothing.
			detail := mustGetRelease(t, ctx, st, tt.p.Infohash)
			if detail.MatchStatus == tt.p.Status && tt.p.Status != "matched" {
				t.Fatalf("status became %q despite the rejection", detail.MatchStatus)
			}
		})
	}
}

// The transition table is checked first, so a target that was never reachable
// from the current state is a conflict whatever the body carried.
func TestOperatorStatus_TransitionsOutsideTheTableStayConflicts(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)

	// unmatched -> matched: submit owns every route into matched from the
	// queue, and it is claim-fenced. A ref does not buy a way around that.
	seedRelease(t, ctx, st, opIH1, "op-table-unmatched", clock.now)
	err := st.SetStatus(ctx, store.SetStatusParams{Infohash: opIH1, Status: "matched", Ref: "tvdb:1"})
	if !errors.Is(err, store.ErrInvalidTransition) {
		t.Fatalf("unmatched -> matched err = %v, want ErrInvalidTransition", err)
	}
	// Same transition with no ref is still the conflict, not a ref error:
	// the table is consulted before the ref rules.
	err = st.SetStatus(ctx, store.SetStatusParams{Infohash: opIH1, Status: "matched"})
	if !errors.Is(err, store.ErrInvalidTransition) {
		t.Fatalf("unmatched -> matched (no ref) err = %v, want ErrInvalidTransition", err)
	}
	// unmatched -> unmatched is a no-op, not a transition, so requeueing a
	// release already in the queue changes nothing rather than erroring.
	if err := st.SetStatus(ctx, store.SetStatusParams{Infohash: opIH1, Status: "unmatched"}); err != nil {
		t.Fatalf("unmatched -> unmatched err = %v, want an idempotent no-op", err)
	}

	// suppressed -> unmatched: requeue is exhausted's alone. A suppressed
	// release was decided against, not abandoned.
	suppressOne(t, ctx, st, clock, opIH2, "op-table-suppressed")
	err = st.SetStatus(ctx, store.SetStatusParams{Infohash: opIH2, Status: "unmatched"})
	if !errors.Is(err, store.ErrInvalidTransition) {
		t.Fatalf("suppressed -> unmatched err = %v, want ErrInvalidTransition", err)
	}
	// suppressed -> dead: dead is a downloader verdict on a matched release.
	err = st.SetStatus(ctx, store.SetStatusParams{Infohash: opIH2, Status: "dead"})
	if !errors.Is(err, store.ErrInvalidTransition) {
		t.Fatalf("suppressed -> dead err = %v, want ErrInvalidTransition", err)
	}

	// matched -> unmatched: there is no un-matching by hand; a wrong match is
	// corrected with a new ref.
	matchWith(t, ctx, st, clock, opIH3, "op-table-matched", "tvdb:1", ptr(0.9))
	err = st.SetStatus(ctx, store.SetStatusParams{Infohash: opIH3, Status: "unmatched"})
	if !errors.Is(err, store.ErrInvalidTransition) {
		t.Fatalf("matched -> unmatched err = %v, want ErrInvalidTransition", err)
	}
	// dead stays terminal: neither operator row reopens it.
	if err := st.SetStatus(ctx, store.SetStatusParams{Infohash: opIH3, Status: "dead"}); err != nil {
		t.Fatalf("matched -> dead: %v", err)
	}
	for _, target := range []store.SetStatusParams{
		{Infohash: opIH3, Status: "matched", Ref: "tvdb:2"},
		{Infohash: opIH3, Status: "unmatched"},
	} {
		if err := st.SetStatus(ctx, target); !errors.Is(err, store.ErrInvalidTransition) {
			t.Fatalf("dead -> %s err = %v, want ErrInvalidTransition", target.Status, err)
		}
	}
}

// The same operator rows over HTTP, with the codes the SPA switches on.
func TestOperatorStatus_RESTContract(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)
	handler := rest.New(st)

	exhaustOne(t, ctx, st, clock, opIH1, "op-rest-exhausted")
	suppressOne(t, ctx, st, clock, opIH2, "op-rest-suppressed")
	seedRelease(t, ctx, st, opIH3, "op-rest-unmatched", clock.now)

	putStatus := func(t *testing.T, ih, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/releases/v1/"+ih+"/status",
			strings.NewReader(body)).WithContext(ctx)
		handler.ServeHTTP(rec, req)
		return rec
	}

	t.Run("hand match succeeds", func(t *testing.T) {
		rec := putStatus(t, opIH2, `{"status":"matched","ref":"tvdb:370070","reason":"hand matched"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; response %s", rec.Code, rec.Body.String())
		}
		assertOperatorMatch(t, mustGetRelease(t, ctx, st, opIH2), "tvdb:370070", "hand matched")
	})

	t.Run("requeue succeeds", func(t *testing.T) {
		rec := putStatus(t, opIH1, `{"status":"unmatched"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; response %s", rec.Code, rec.Body.String())
		}
		if got := mustGetRelease(t, ctx, st, opIH1); got.MatchStatus != "unmatched" || got.AttemptCount != 0 {
			t.Fatalf("release = status %q attempts %d, want unmatched with 0 attempts", got.MatchStatus, got.AttemptCount)
		}
	})

	for _, tt := range []struct {
		name, infohash, body string
		wantStatus           int
		wantKind             string
	}{
		{
			name: "hand match with no ref", infohash: opIH2, body: `{"status":"matched"}`,
			wantStatus: http.StatusBadRequest, wantKind: api.KindInvalidRequest,
		},
		{
			// A ref on a target that takes none is a malformed body whatever
			// state the row is in — opIH1 is already unmatched here.
			name: "requeue with a ref", infohash: opIH1, body: `{"status":"unmatched","ref":"tvdb:1"}`,
			wantStatus: http.StatusBadRequest, wantKind: api.KindInvalidRequest,
		},
		{
			name: "hand match with a malformed ref", infohash: opIH2, body: `{"status":"matched","ref":"nope"}`,
			wantStatus: http.StatusBadRequest, wantKind: api.KindInvalidRef,
		},
		{
			// opIH3 is unmatched: the table has no row out of it.
			name: "transition outside the table", infohash: opIH3, body: `{"status":"matched","ref":"tvdb:1"}`,
			wantStatus: http.StatusConflict, wantKind: api.KindInvalidTransition,
		},
		{
			// The claim fence is the table's: unmatched -> suppressed has
			// no row, so submit stays the only route out of the queue.
			name: "suppress out of the queue is a table miss", infohash: opIH3, body: `{"status":"suppressed"}`,
			wantStatus: http.StatusConflict, wantKind: api.KindInvalidTransition,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := putStatus(t, tt.infohash, tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; response %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			var body api.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q; response %s", body.Kind, tt.wantKind, rec.Body.String())
			}
		})
	}

	t.Run("a repeat hand match appends no second audit row", func(t *testing.T) {
		before := mustGetRelease(t, ctx, st, opIH2)
		clock.Advance(time.Minute)
		rec := putStatus(t, opIH2, `{"status":"matched","ref":"tvdb:370070","reason":"again"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; response %s", rec.Code, rec.Body.String())
		}
		if after := mustGetRelease(t, ctx, st, opIH2); len(after.MatchEvents) != len(before.MatchEvents) {
			t.Fatalf("match_events grew from %d to %d on a repeat PUT",
				len(before.MatchEvents), len(after.MatchEvents))
		}
	})
}

// Suppress is the attention surface's discard: an exhausted release the
// operator decides against goes to suppressed rather than being matched or
// requeued. The accounting that led there is preserved, and suppression out
// of unmatched stays submit-only — the operator discards what the matcher
// gave up on, not what is still in the queue.
func TestOperatorStatus_SuppressAnExhaustedRelease(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)

	exhaustOne(t, ctx, st, clock, opIH1, "op-suppress")
	attempts := mustGetRelease(t, ctx, st, opIH1).AttemptCount

	// suppressed takes no ref, whatever the source state.
	err := st.SetStatus(ctx, store.SetStatusParams{Infohash: opIH1, Status: "suppressed", Ref: "tvdb:1"})
	if !errors.Is(err, store.ErrRefForbidden) {
		t.Fatalf("suppress with ref err = %v, want ErrRefForbidden", err)
	}

	if err := st.SetStatus(ctx, store.SetStatusParams{
		Infohash: opIH1, Status: "suppressed", Reason: "batch posting, not library content",
	}); err != nil {
		t.Fatalf("SetStatus exhausted -> suppressed: %v", err)
	}
	detail := mustGetRelease(t, ctx, st, opIH1)
	if detail.MatchStatus != "suppressed" {
		t.Fatalf("MatchStatus = %q, want suppressed", detail.MatchStatus)
	}
	if detail.AttemptCount != attempts {
		t.Fatalf("AttemptCount = %d, want %d preserved — suppression is a verdict, not a reset",
			detail.AttemptCount, attempts)
	}
	event := lastMatchEvent(t, detail)
	if event.Status != "suppressed" || event.Ref != nil || event.Confidence != nil {
		t.Fatalf("last match_event = %+v, want status=suppressed with nil ref and confidence", event)
	}
	if event.Reason == nil || *event.Reason != "batch posting, not library content" {
		t.Fatalf("last match_event reason = %v, want the operator's reason", event.Reason)
	}

	// A retried PUT is a no-op, not a second audit row.
	if err := st.SetStatus(ctx, store.SetStatusParams{Infohash: opIH1, Status: "suppressed"}); err != nil {
		t.Fatalf("repeat suppress err = %v, want an idempotent no-op", err)
	}
	if after := mustGetRelease(t, ctx, st, opIH1); len(after.MatchEvents) != len(detail.MatchEvents) {
		t.Fatalf("match_events grew from %d to %d on a repeat PUT",
			len(detail.MatchEvents), len(after.MatchEvents))
	}

	// unmatched -> suppressed has no row: submit remains the only route out
	// of the queue, so the claim fence cannot be bypassed by hand.
	seedRelease(t, ctx, st, opIH2, "op-suppress-unmatched", clock.now)
	err = st.SetStatus(ctx, store.SetStatusParams{Infohash: opIH2, Status: "suppressed"})
	if !errors.Is(err, store.ErrInvalidTransition) {
		t.Fatalf("unmatched -> suppressed err = %v, want ErrInvalidTransition", err)
	}
}

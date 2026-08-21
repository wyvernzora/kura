//go:build conformance

package conformance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/internal/dispatch"
	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

const (
	precIHMatched    = "1111111111111111111111111111111111111111"
	precIHSuppressed = "2222222222222222222222222222222222222222"
	precIHUnrelated  = "3333333333333333333333333333333333333333"
	precIHExhausted  = "4444444444444444444444444444444444444444"
	precIHUnmatched  = "5555555555555555555555555555555555555555"
	precIHTarget     = "6666666666666666666666666666666666666666"
)

// seedTitled ingests one release under an exact title; the shared seed helper
// derives its title from the source id, and these tests are about titles.
func seedTitled(t *testing.T, ctx context.Context, st store.Store, ih, sourceID, title string, published time.Time) {
	t.Helper()
	_, err := st.IngestN(ctx, store.IngestParams{
		Infohash:    ih,
		Source:      "dmhy",
		SourceID:    sourceID,
		Title:       title,
		URL:         "https://example.invalid/" + sourceID,
		Magnet:      "magnet:?xt=urn:btih:" + ih,
		SizeBytes:   1234,
		PublishedAt: published,
	})
	if err != nil {
		t.Fatalf("IngestN %s: %v", sourceID, err)
	}
}

// seedDecided drives one release from ingest through claim to a disposition,
// which is the only route into matched or suppressed, then advances the clock
// so the next seeded release is unambiguously the newest claimable one.
func seedDecided(t *testing.T, ctx context.Context, st store.Store, clock *fakeClock, ih, sourceID, title string, sub store.SubmitParams) {
	t.Helper()
	seedTitled(t, ctx, st, ih, sourceID, title, clock.now)
	claimed := claimOne(t, ctx, st, 60)
	if claimed.Infohash != ih {
		t.Fatalf("claimed %s, want %s", claimed.Infohash, ih)
	}
	sub.Infohash = ih
	sub.ClaimToken = claimed.ClaimToken
	if err := st.Submit(ctx, sub); err != nil {
		t.Fatalf("Submit %s as %s: %v", sourceID, sub.Status, err)
	}
	clock.Advance(time.Minute)
}

func claimTargetPrecedents(t *testing.T, ctx context.Context, st store.Store) []api.ClaimPrecedent {
	t.Helper()
	res, err := dispatch.New(st).Claim(ctx, mustJSON(t, map[string]any{"limit": 1, "leaseSeconds": 60}))
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	var claimed api.ClaimResponse
	if err := json.Unmarshal(res, &claimed); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if len(claimed.Items) != 1 {
		t.Fatalf("claim returned %d items, want 1", len(claimed.Items))
	}
	if claimed.Items[0].Infohash != precIHTarget {
		t.Fatalf("claimed %s, want the target %s", claimed.Items[0].Infohash, precIHTarget)
	}
	return claimed.Items[0].Precedents
}

// A claim ships the decided releases nearest the claimed title so the matcher
// can carry a prior manual decision forward. It ships scores, not a verdict:
// the cutoff is matcher policy.
func TestClaimPrecedents_RanksDecidedReleasesByTitleSimilarity(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)

	seedDecided(t, ctx, st, clock, precIHMatched, "prec-matched", "[GroupA] Show Title - 01",
		store.SubmitParams{Status: "matched", Ref: "tvdb:4242", Confidence: ptr(0.93)})
	seedDecided(t, ctx, st, clock, precIHSuppressed, "prec-suppressed", "[BadGroup] Show Title - 01v2",
		store.SubmitParams{Status: "suppressed", Reason: "re-encode of an existing release"})
	seedDecided(t, ctx, st, clock, precIHUnrelated, "prec-unrelated", "[Zeta] Completely Other Franchise Movie",
		store.SubmitParams{Status: "matched", Ref: "tvdb:9001"})

	// An exhausted release: decided-looking, but never manually decided, so it
	// carries nothing to precede a new claim with.
	seedTitled(t, ctx, st, precIHExhausted, "prec-exhausted", "[GroupA] Show Title - 03", clock.now)
	for i := 0; i < 3; i++ {
		claimed := claimOne(t, ctx, st, 60)
		if claimed.Infohash != precIHExhausted {
			t.Fatalf("claimed %s, want %s", claimed.Infohash, precIHExhausted)
		}
		if err := st.Submit(ctx, store.SubmitParams{
			Infohash:   precIHExhausted,
			ClaimToken: claimed.ClaimToken,
			Status:     "unmatched",
			Reason:     "no match",
		}); err != nil {
			t.Fatalf("Submit unmatched attempt %d: %v", i+1, err)
		}
		clock.Advance(61 * time.Second)
	}
	qs, err := st.QueueStats(ctx)
	if err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if qs.Exhausted != 1 {
		t.Fatalf("QueueStats.Exhausted = %d, want 1; the exclusion assertion would be vacuous", qs.Exhausted)
	}

	// A still-undecided release with an equally close title, and the target
	// itself: neither may show up as anyone's precedent.
	seedTitled(t, ctx, st, precIHUnmatched, "prec-unmatched", "[GroupA] Show Title - 04", clock.now)
	clock.Advance(time.Minute)
	seedTitled(t, ctx, st, precIHTarget, "prec-target", "[GroupA] Show Title - 02", clock.now)

	precedents := claimTargetPrecedents(t, ctx, st)
	if len(precedents) == 0 {
		t.Fatal("precedents is empty, want the decided releases nearest the claimed title")
	}
	if len(precedents) > 3 {
		t.Fatalf("precedents len = %d, want at most the top 3", len(precedents))
	}

	at := map[string]int{}
	for i, p := range precedents {
		at[p.Infohash] = i
		if i > 0 && precedents[i-1].Similarity < p.Similarity {
			t.Fatalf("precedents not ordered by similarity desc: %v then %v",
				precedents[i-1].Similarity, p.Similarity)
		}
	}
	for name, ih := range map[string]string{
		"exhausted":                  precIHExhausted,
		"unmatched":                  precIHUnmatched,
		"the claimed release itself": precIHTarget,
	} {
		if _, ok := at[ih]; ok {
			t.Fatalf("precedents included %s (%s), want only matched and suppressed releases", name, ih)
		}
	}

	matchedAt, ok := at[precIHMatched]
	if !ok {
		t.Fatalf("precedents %+v missing the matched release %s", precedents, precIHMatched)
	}
	suppressedAt, ok := at[precIHSuppressed]
	if !ok {
		t.Fatalf("precedents %+v missing the suppressed release %s", precedents, precIHSuppressed)
	}
	if matchedAt > suppressedAt {
		t.Fatalf("same-group precedent ranked below the other group's: %+v", precedents)
	}
	if unrelatedAt, ok := at[precIHUnrelated]; ok && unrelatedAt != len(precedents)-1 {
		t.Fatalf("unrelated title ranked at %d of %d, want last if present", unrelatedAt, len(precedents))
	}

	matched := precedents[matchedAt]
	suppressed := precedents[suppressedAt]
	for _, p := range []api.ClaimPrecedent{matched, suppressed} {
		if p.Similarity <= 0 {
			t.Fatalf("precedent %q similarity = %v, want a positive score", p.Title, p.Similarity)
		}
	}
	if matched.MatchStatus != api.MatchStatusMatched {
		t.Fatalf("matched precedent status = %q, want matched", matched.MatchStatus)
	}
	if matched.Title != "[GroupA] Show Title - 01" {
		t.Fatalf("matched precedent title = %q", matched.Title)
	}
	if matched.Ref == nil || *matched.Ref != "tvdb:4242" {
		t.Fatalf("matched precedent ref = %v, want tvdb:4242", matched.Ref)
	}

	if suppressed.MatchStatus != api.MatchStatusSuppressed {
		t.Fatalf("suppressed precedent status = %q, want suppressed", suppressed.MatchStatus)
	}
	// The reason is why a suppression is worth carrying forward; it lives in
	// match_events, not on the release row.
	if suppressed.Reason == nil || *suppressed.Reason != "re-encode of an existing release" {
		t.Fatalf("suppressed precedent reason = %v, want the submitted reason", suppressed.Reason)
	}
	if suppressed.Ref != nil {
		t.Fatalf("suppressed precedent ref = %v, want null", *suppressed.Ref)
	}
}

func TestClaimPrecedents_EmptyArrayWhenNothingIsDecided(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)

	seedTitled(t, ctx, st, precIHTarget, "prec-target", "[GroupA] Show Title - 02", clock.now)

	res, err := dispatch.New(st).Claim(ctx, mustJSON(t, map[string]any{"limit": 1, "leaseSeconds": 60}))
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	var body struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(res, &body); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("claim returned %d items, want 1", len(body.Items))
	}
	assertJSONArray(t, body.Items[0], "precedents")
	if got := string(body.Items[0]["precedents"]); got != "[]" {
		t.Fatalf("precedents = %s, want an empty array", got)
	}
}

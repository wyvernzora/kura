//go:build conformance

package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/internal/dispatch"
	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// matchOne seeds a release and drives it all the way to matched, which is the
// only state the transition table accepts as a source.
func matchOne(t *testing.T, ctx context.Context, st store.Store, ih, title string, now time.Time) {
	t.Helper()
	seedRelease(t, ctx, st, ih, title, now)
	claimed := claimOne(t, ctx, st, 60)
	if err := st.Submit(ctx, store.SubmitParams{
		Infohash:   claimed.Infohash,
		ClaimToken: claimed.ClaimToken,
		Status:     "matched",
		Ref:        "tvdb:4242",
		Confidence: ptr(0.9),
	}); err != nil {
		t.Fatalf("Submit matched: %v", err)
	}
}

func TestDeadStatus_MatchedToDeadIsAllowedAndAudited(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)
	matchOne(t, ctx, st, apiIH1, "dead-1", clock.now)

	clock.Advance(time.Minute)
	if err := st.SetStatus(ctx, store.SetStatusParams{
		Infohash: apiIH1,
		Status:   "dead",
		Reason:   "stalled at 0% for 14 days",
	}); err != nil {
		t.Fatalf("SetStatus matched -> dead: %v", err)
	}

	got, err := st.GetRelease(ctx, apiIH1)
	if err != nil {
		t.Fatalf("GetRelease: %v", err)
	}
	if got.MatchStatus != "dead" {
		t.Fatalf("MatchStatus = %q, want dead", got.MatchStatus)
	}
	// The match history is what keeps a dead release from being re-selected;
	// erasing it would make the row indistinguishable from a never-matched one.
	if got.Ref == nil || *got.Ref != "tvdb:4242" {
		t.Fatalf("Ref = %v, want tvdb:4242 preserved", got.Ref)
	}
	if got.FirstMatchedAt == nil {
		t.Fatal("FirstMatchedAt = nil, want the original match preserved")
	}
	// The reason lands in match_events, the existing audit trail — the
	// transition adds no columns.
	last := got.MatchEvents[len(got.MatchEvents)-1]
	if last.Status != "dead" {
		t.Fatalf("last match_event status = %q, want dead", last.Status)
	}
	if last.Reason == nil || *last.Reason != "stalled at 0% for 14 days" {
		t.Fatalf("last match_event reason = %v, want the submitted reason", last.Reason)
	}
}

func TestDeadStatus_RepeatIsAnIdempotentNoOp(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)
	matchOne(t, ctx, st, apiIH1, "dead-2", clock.now)

	p := store.SetStatusParams{Infohash: apiIH1, Status: "dead", Reason: "first"}
	if err := st.SetStatus(ctx, p); err != nil {
		t.Fatalf("first SetStatus: %v", err)
	}
	before, err := st.GetRelease(ctx, apiIH1)
	if err != nil {
		t.Fatalf("GetRelease: %v", err)
	}

	clock.Advance(time.Minute)
	p.Reason = "second"
	if err := st.SetStatus(ctx, p); err != nil {
		t.Fatalf("repeat SetStatus: %v", err)
	}
	after, err := st.GetRelease(ctx, apiIH1)
	if err != nil {
		t.Fatalf("GetRelease after repeat: %v", err)
	}
	// A retried PUT must not append a second audit row for a transition
	// that already happened.
	if len(after.MatchEvents) != len(before.MatchEvents) {
		t.Fatalf("match_events grew from %d to %d on a repeat call",
			len(before.MatchEvents), len(after.MatchEvents))
	}
}

func TestDeadStatus_TransitionsOutsideTheTableAreRejected(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)

	// unmatched -> dead: not in the table. Marking a release dead before
	// anyone matched it would strand it out of the claim queue.
	seedRelease(t, ctx, st, apiIH1, "dead-3", clock.now)
	err := st.SetStatus(ctx, store.SetStatusParams{Infohash: apiIH1, Status: "dead"})
	if !errors.Is(err, store.ErrInvalidTransition) {
		t.Fatalf("unmatched -> dead err = %v, want ErrInvalidTransition", err)
	}

	// dead -> matched: submit owns every route into matched, and there is
	// no recovery transition in v1.
	matchOne(t, ctx, st, apiIH2, "dead-4", clock.now)
	if err := st.SetStatus(ctx, store.SetStatusParams{Infohash: apiIH2, Status: "dead"}); err != nil {
		t.Fatalf("SetStatus matched -> dead: %v", err)
	}
	err = st.SetStatus(ctx, store.SetStatusParams{Infohash: apiIH2, Status: "matched"})
	if !errors.Is(err, store.ErrInvalidTransition) {
		t.Fatalf("dead -> matched err = %v, want ErrInvalidTransition", err)
	}

	// A release that does not exist is not found, not an invalid transition.
	err = st.SetStatus(ctx, store.SetStatusParams{Infohash: apiIH3, Status: "dead"})
	if !errors.Is(err, store.ErrNoSuchRelease) {
		t.Fatalf("missing release err = %v, want ErrNoSuchRelease", err)
	}
}

func TestDeadStatus_MagnetGateAndQueueCounts(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	st := newConformanceStoreWithClock(t, clock)
	matchOne(t, ctx, st, apiIH1, "dead-5", clock.now)

	// Matched: the magnet resolves. This is the pre-condition that makes
	// the post-dead assertion meaningful rather than vacuous.
	magnets, err := st.ResolveMagnets(ctx, []string{apiIH1})
	if err != nil {
		t.Fatalf("ResolveMagnets while matched: %v", err)
	}
	if _, ok := magnets[apiIH1]; !ok {
		t.Fatal("magnet did not resolve while matched; the gate test would prove nothing")
	}

	if err := st.SetStatus(ctx, store.SetStatusParams{Infohash: apiIH1, Status: "dead"}); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	// Dead: the magnet takes the lookup-missed path, so a stale agent
	// selection cannot re-fetch and re-add a pruned torrent.
	magnets, err = st.ResolveMagnets(ctx, []string{apiIH1})
	if err != nil {
		t.Fatalf("ResolveMagnets after dead: %v", err)
	}
	if _, ok := magnets[apiIH1]; ok {
		t.Fatal("magnet still resolved for a dead release, want it gated")
	}

	qs, err := st.QueueStats(ctx)
	if err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if qs.Dead != 1 {
		t.Fatalf("QueueStats.Dead = %d, want 1", qs.Dead)
	}
	if qs.Matched != 0 {
		t.Fatalf("QueueStats.Matched = %d, want 0 once the release went dead", qs.Matched)
	}

	// The dead release is excluded from the agent-facing list by the
	// existing status predicate — no query change was needed.
	d := dispatch.New(st)
	res, err := d.ListReleases(ctx, mustJSON(t, map[string]any{"ref": "tvdb:4242"}))
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	var listed api.ReleaseList
	if err := json.Unmarshal(res, &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed.Items) != 0 {
		t.Fatalf("list returned %d items, want the dead release excluded", len(listed.Items))
	}
}

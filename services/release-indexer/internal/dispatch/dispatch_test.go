package dispatch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// recordingStore is the seam these tests assert across: a rejected request must
// never reach it, and an accepted one must arrive with its filters intact.
type recordingStore struct {
	called       bool
	releaseQuery store.ReleaseQuery
	setStatus    store.SetStatusParams
}

func (s *recordingStore) ListReleases(_ context.Context, q store.ReleaseQuery) (store.ReleasePage, error) {
	s.called, s.releaseQuery = true, q
	return store.ReleasePage{}, nil
}

func (s *recordingStore) SetStatus(_ context.Context, p store.SetStatusParams) error {
	s.called, s.setStatus = true, p
	return nil
}

func (s *recordingStore) Ping(context.Context) error { return nil }
func (s *recordingStore) IngestN(context.Context, store.IngestParams) (store.IngestOutcome, error) {
	return store.IngestOutcome{}, nil
}
func (s *recordingStore) Claim(context.Context, store.ClaimParams) (store.ClaimResult, error) {
	return store.ClaimResult{}, nil
}
func (s *recordingStore) Submit(context.Context, store.SubmitParams) error { return nil }
func (s *recordingStore) QueueStats(context.Context) (store.QueueStats, error) {
	return store.QueueStats{}, nil
}
func (s *recordingStore) CatalogStats(context.Context) (store.CatalogStats, error) {
	return store.CatalogStats{}, nil
}
func (s *recordingStore) GetRelease(context.Context, string) (store.ReleaseDetail, error) {
	return store.ReleaseDetail{}, nil
}
func (s *recordingStore) ResolveMagnets(context.Context, []string) (map[string]string, error) {
	return nil, nil
}
func (s *recordingStore) Close() error { return nil }

func TestListReleasesRejectsFiltersItCannotServe(t *testing.T) {
	since := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for name, req := range map[string]ListReleasesRequest{
		"unknown status":            {Statuses: []string{"defer"}},
		"empty status element":      {Statuses: []string{"matched", ""}},
		"status with since":         {Statuses: []string{"exhausted"}, Since: &since},
		"maxConfidence with since":  {MaxConfidence: ptr(0.75), Since: &since},
		"maxConfidence of zero":     {MaxConfidence: ptr(0.0)},
		"negative maxConfidence":    {MaxConfidence: ptr(-0.5)},
		"maxConfidence above one":   {MaxConfidence: ptr(1.5)},
		"unknown status among good": {Statuses: []string{"matched", "nonsense", "dead"}},
	} {
		t.Run(name, func(t *testing.T) {
			st := &recordingStore{}
			_, err := New(st).ListReleasesTyped(context.Background(), req)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if ErrorKind(err) != api.KindInvalidRequest {
				t.Fatalf("kind = %q, want %q", ErrorKind(err), api.KindInvalidRequest)
			}
			if st.called {
				t.Fatalf("store was queried with %+v, want the request refused first", st.releaseQuery)
			}
		})
	}
}

// The boundary between "rejected" and "passed to the store" is worth pinning
// from both sides: these are the filters the endpoint does serve, so a
// validation rule that over-reaches shows up here as a rejection, and a filter
// dropped on the way down shows up as a missing field.
func TestListReleasesForwardsTheFiltersItServes(t *testing.T) {
	st := &recordingStore{}
	_, err := New(st).ListReleasesTyped(context.Background(), ListReleasesRequest{
		Ref:           "tvdb:123",
		Statuses:      []string{"exhausted", "suppressed"},
		MaxConfidence: ptr(0.75),
		Limit:         8,
		Cursor:        "abc",
	})
	if err != nil {
		t.Fatalf("ListReleasesTyped: %v", err)
	}
	q := st.releaseQuery
	if len(q.Statuses) != 2 || q.Statuses[0] != "exhausted" || q.Statuses[1] != "suppressed" {
		t.Fatalf("statuses = %v, want them forwarded", q.Statuses)
	}
	if q.MaxConfidence == nil || *q.MaxConfidence != 0.75 {
		t.Fatalf("maxConfidence = %v, want 0.75", q.MaxConfidence)
	}
	if q.Ref != "tvdb:123" || q.Limit != 8 || q.Cursor != "abc" {
		t.Fatalf("query = %+v, want ref/limit/cursor preserved alongside the new filters", q)
	}
}

func TestListReleasesAcceptsTheFiltersItServes(t *testing.T) {
	for name, req := range map[string]ListReleasesRequest{
		"every status":            {Statuses: []string{"unmatched", "matched", "suppressed", "exhausted", "dead"}},
		"maxConfidence at one":    {MaxConfidence: ptr(1.0)},
		"status + maxConfidence":  {Statuses: []string{"matched"}, MaxConfidence: ptr(0.75)},
		"no filters at all":       {},
		"since without a filter":  {Since: ptr(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))},
		"tiny maxConfidence":      {MaxConfidence: ptr(0.0001)},
		"repeated status entries": {Statuses: []string{"matched", "matched"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateListReleases(req); err != nil {
				t.Fatalf("validateListReleases(%+v) = %v, want nil", req, err)
			}
		})
	}
}

// The endpoint's vocabulary is narrower than MatchStatus: suppressed is the
// matcher's to set through the claim-fenced submit path.
func TestSetStatusRejectsTargetsTheEndpointDoesNotOffer(t *testing.T) {
	for _, status := range []api.MatchStatus{"suppressed", "", "defer", "DEAD"} {
		t.Run(string(status), func(t *testing.T) {
			st := &recordingStore{}
			err := New(st).SetStatusTyped(context.Background(), "abc",
				api.SetStatusRequest{Status: status})
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if st.called {
				t.Fatalf("store was called with %+v, want the request refused first", st.setStatus)
			}
		})
	}
}

// The ref rides down to the store untouched — this layer neither validates nor
// rewrites it; the transition table does, after it has approved the transition.
func TestSetStatusForwardsTheRefToTheStore(t *testing.T) {
	st := &recordingStore{}
	err := New(st).SetStatusTyped(context.Background(), "abc", api.SetStatusRequest{
		Status: api.MatchStatusMatched,
		Ref:    "tvdb:370070",
		Reason: "hand matched",
	})
	if err != nil {
		t.Fatalf("SetStatusTyped: %v", err)
	}
	if st.setStatus.Ref != "tvdb:370070" || st.setStatus.Status != "matched" {
		t.Fatalf("params = %+v, want matched with the ref preserved", st.setStatus)
	}
}

// The ref-rule sentinels are request errors, not conflicts: the transition was
// on offer, the body was wrong.
func TestErrorKindMapsRefRuleViolationsToInvalidRequest(t *testing.T) {
	for _, sentinel := range []error{store.ErrRefRequired, store.ErrRefForbidden} {
		wrapped := fmt.Errorf("set_status abc: %w", sentinel)
		if got := ErrorKind(wrapped); got != api.KindInvalidRequest {
			t.Fatalf("ErrorKind(%v) = %q, want %q", sentinel, got, api.KindInvalidRequest)
		}
	}
	// Contrast: a rejected transition stays a conflict, so the two are not
	// collapsing into one another.
	if got := ErrorKind(fmt.Errorf("x: %w", store.ErrInvalidTransition)); got != api.KindInvalidTransition {
		t.Fatalf("ErrorKind(ErrInvalidTransition) = %q, want %q", got, api.KindInvalidTransition)
	}
}

func ptr[T any](v T) *T { return &v }

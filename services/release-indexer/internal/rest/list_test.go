package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// The query string is the whole input surface for this endpoint, so the
// mapping onto the store query is what is worth pinning. An omitted limit
// stays zero so the store applies its own default rather than the handler
// guessing one.
func TestListReleasesMapsQueryParams(t *testing.T) {
	st := &fakeStore{}
	h := New(st)

	req := httptest.NewRequest(http.MethodGet,
		"/api/releases/v1?ref=tvdb:123&limit=7&since=2026-06-24T12:00:00Z&cursor=abc", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; response %s", rec.Code, rec.Body.String())
	}
	if st.releaseQuery.Ref != "tvdb:123" || st.releaseQuery.Limit != 7 || st.releaseQuery.Cursor != "abc" {
		t.Fatalf("query = %+v, want ref/limit/cursor forwarded", st.releaseQuery)
	}
	if st.releaseQuery.Since == nil || !st.releaseQuery.Since.Equal(time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("since = %v, want 2026-06-24T12:00:00Z", st.releaseQuery.Since)
	}

	// Items is never null: an empty page still encodes as [].
	var body struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Items == nil {
		t.Fatal("items = null, want []")
	}
}

func TestListReleasesRejectsBadQueryParams(t *testing.T) {
	for _, q := range []string{"?limit=nope", "?limit=-1", "?since=yesterday"} {
		rec := httptest.NewRecorder()
		New(&fakeStore{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/releases/v1"+q, http.NoBody))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", q, rec.Code)
		}
	}
}

// `status` is a comma-separated list and `maxConfidence` a bare float; both are
// the operator surface's filters and both compose with ref, limit, and cursor.
func TestListReleasesMapsAttentionFilters(t *testing.T) {
	for _, tt := range []struct {
		name          string
		query         string
		wantStatuses  []string
		wantMaxConfid *float64
	}{
		{
			name:         "single status",
			query:        "?status=exhausted",
			wantStatuses: []string{"exhausted"},
		},
		{
			name:         "comma separated statuses",
			query:        "?status=exhausted,suppressed",
			wantStatuses: []string{"exhausted", "suppressed"},
		},
		{
			name:          "confidence ceiling alone",
			query:         "?maxConfidence=0.75",
			wantMaxConfid: ptrTo(0.75),
		},
		{
			name:          "both filters with ref and limit",
			query:         "?ref=tvdb:123&limit=8&status=matched&maxConfidence=0.75",
			wantStatuses:  []string{"matched"},
			wantMaxConfid: ptrTo(0.75),
		},
		{
			// The default is what n8n and the CLI depend on: matched only,
			// expressed as no status filter at all.
			name:  "no filters is the matched-only default",
			query: "",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := &fakeStore{}
			rec := httptest.NewRecorder()
			New(st).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/releases/v1"+tt.query, http.NoBody))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; response %s", rec.Code, rec.Body.String())
			}
			if !slices.Equal(st.releaseQuery.Statuses, tt.wantStatuses) {
				t.Fatalf("statuses = %v, want %v", st.releaseQuery.Statuses, tt.wantStatuses)
			}
			switch {
			case tt.wantMaxConfid == nil && st.releaseQuery.MaxConfidence != nil:
				t.Fatalf("maxConfidence = %v, want none", *st.releaseQuery.MaxConfidence)
			case tt.wantMaxConfid != nil && st.releaseQuery.MaxConfidence == nil:
				t.Fatalf("maxConfidence = nil, want %v", *tt.wantMaxConfid)
			case tt.wantMaxConfid != nil && *st.releaseQuery.MaxConfidence != *tt.wantMaxConfid:
				t.Fatalf("maxConfidence = %v, want %v", *st.releaseQuery.MaxConfidence, *tt.wantMaxConfid)
			}
		})
	}
}

func TestListReleasesRejectsBadAttentionFilters(t *testing.T) {
	for _, tt := range []struct {
		name, query string
		// wantMessage pins which layer refused. A float that does not parse
		// reads as 0, which the range check would also reject — the message
		// is the only thing that tells the parse guard apart from it.
		wantMessage string
	}{
		{name: "unparseable confidence", query: "?maxConfidence=nope", wantMessage: "maxConfidence must be a number in (0,1]"},
		{name: "confidence of zero", query: "?maxConfidence=0"},
		{name: "confidence above one", query: "?maxConfidence=1.5"},
		{name: "negative confidence", query: "?maxConfidence=-0.2"},
		{name: "status outside the vocabulary", query: "?status=defer"},
		{name: "empty element in the status list", query: "?status=exhausted,"},
		// `since` is the delta path and is matched-only by definition, so
		// pairing it with either filter is a contradiction.
		{name: "status with since", query: "?status=exhausted&since=2026-06-24T12:00:00Z"},
		{name: "confidence with since", query: "?maxConfidence=0.75&since=2026-06-24T12:00:00Z"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(&fakeStore{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/releases/v1"+tt.query, http.NoBody))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; response %s", rec.Code, rec.Body.String())
			}
			var body api.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Kind != api.KindInvalidRequest {
				t.Fatalf("kind = %q, want %q", body.Kind, api.KindInvalidRequest)
			}
			if tt.wantMessage != "" && body.Message != tt.wantMessage {
				t.Fatalf("message = %q, want %q", body.Message, tt.wantMessage)
			}
		})
	}
}

// matchStatus rides on every row now that the list is not matched-only, so a
// client can render a mixed page without a second lookup.
func TestListReleasesRendersMatchStatusPerRow(t *testing.T) {
	st := &fakeStore{releasePage: store.ReleasePage{Releases: []store.ReleaseItem{{
		Infohash:    "0123456789abcdef0123456789abcdef01234567",
		Title:       "release",
		PublishedAt: time.Unix(1, 0).UTC(),
		MatchStatus: "exhausted",
	}}}}
	rec := httptest.NewRecorder()
	New(st).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/releases/v1?status=exhausted", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; response %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			MatchStatus string `json:"matchStatus"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].MatchStatus != "exhausted" {
		t.Fatalf("items = %+v, want one row carrying matchStatus exhausted", body.Items)
	}
}

func ptrTo[T any](v T) *T { return &v }

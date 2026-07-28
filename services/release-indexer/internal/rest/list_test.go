package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The query string is the whole input surface for this endpoint, so the
// mapping onto the store query is what is worth pinning. An omitted limit
// stays zero so the store applies its own default rather than the handler
// guessing one.
func TestListReleasesMapsQueryParams(t *testing.T) {
	st := &fakeStore{}
	h := New(st)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/releases?ref=tvdb:123&limit=7&since=2026-06-24T12:00:00Z&cursor=abc", http.NoBody)
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
		New(&fakeStore{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/releases"+q, http.NoBody))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", q, rec.Code)
		}
	}
}

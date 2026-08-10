package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relapi "github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

func TestCrawlSource(t *testing.T) {
	var gotPath, gotMethod string
	var gotReq relapi.CrawlRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(relapi.CrawlResult{
			Source:       relapi.SourceNyaa,
			Posts:        100,
			PagesFetched: 2,
			Batch:        relapi.IngestBatch{New: 5, Duplicate: 95},
			Queue:        relapi.QueueCounts{Available: 12},
			NextCursor:   "opaque",
			HasMore:      true,
			StopReason:   relapi.CrawlStopPageBudget,
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.HTTPClient.Timeout = time.Nanosecond
	resp, err := c.CrawlSource(t.Context(), relapi.SourceNyaa, relapi.CrawlRequest{
		PageSize: 100,
		Cursor:   "previous",
		Lookback: "30d",
	})
	if err != nil {
		t.Fatalf("CrawlSource() error = %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/releases/v1/sources/nyaa/crawl" {
		t.Fatalf("request = %s %s, want POST /api/releases/v1/sources/nyaa/crawl", gotMethod, gotPath)
	}
	if gotReq.PageSize != 100 || gotReq.Cursor != "previous" || gotReq.Lookback != "30d" {
		t.Fatalf("request body = %+v", gotReq)
	}
	if resp.NextCursor != "opaque" || resp.Posts != 100 || resp.Batch.New != 5 || resp.Queue.Available != 12 {
		t.Fatalf("response = %+v", resp)
	}
}

func TestCrawlSourceSurfacesErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"kind":"upstream_error","message":"page fetch failed"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).CrawlSource(t.Context(), relapi.SourceDMHY, relapi.CrawlRequest{PageSize: 200})
	if err == nil {
		t.Fatal("expected an error from a 502 envelope")
	}
}

func TestCrawlSourceConnectionHintNamesReleaseIndexer(t *testing.T) {
	c := New("http://127.0.0.1:1")
	_, err := c.CrawlSource(t.Context(), relapi.SourceDMHY, relapi.CrawlRequest{PageSize: 200})
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if !strings.Contains(err.Error(), "cannot reach release indexer") ||
		!strings.Contains(err.Error(), "suite gateway") {
		t.Fatalf("error = %q", err)
	}
}

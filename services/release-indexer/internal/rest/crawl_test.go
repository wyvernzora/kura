package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/crawl"
)

type fakeChunkCrawler struct {
	pageSize int
	cursor   string
	lookback time.Duration
	result   crawl.CrawlResponse
	err      error
}

func (f *fakeChunkCrawler) CrawlChunk(_ context.Context, pageSize int, cursor string, lookback time.Duration) (crawl.CrawlResponse, error) {
	f.pageSize = pageSize
	f.cursor = cursor
	f.lookback = lookback
	return f.result, f.err
}

func crawlRequest(t *testing.T, h *Handler, source, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sources/"+source+"/crawl", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCrawlEndpointFetchesIngestsAndReportsBounds(t *testing.T) {
	older := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	crawler := &fakeChunkCrawler{result: crawl.CrawlResponse{
		Posts: []api.RawPost{
			{Source: api.SourceNyaa, SourceID: "1", Title: "a", Magnet: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", PublishedAt: newer},
			{Source: api.SourceNyaa, SourceID: "2", Title: "b", Magnet: "magnet:?xt=urn:btih:abcdefabcdefabcdefabcdefabcdefabcdefabcd", PublishedAt: older},
			{Source: api.SourceNyaa, SourceID: "3", Title: "undated", Magnet: "magnet:?xt=urn:btih:1111111111111111111111111111111111111111"},
		},
		NextCursor:   "next",
		HasMore:      true,
		StopReason:   crawl.StopPageBudget,
		PagesFetched: 2,
		LastPage:     3,
	}}
	st := &fakeStore{
		ingestOutcome: store.IngestOutcome{New: true},
		queueStats:    store.QueueStats{Available: 12},
	}
	h := New(st)
	h.RegisterCrawler(api.SourceNyaa, crawler)

	cursor := crawl.FormatCursor(api.SourceNyaa, 2, 10)
	rec := crawlRequest(t, h, api.SourceNyaa,
		`{"pageSize":3,"cursor":"`+cursor+`","lookback":"30d"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp api.CrawlResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if crawler.pageSize != 3 || crawler.cursor != cursor || crawler.lookback != 30*24*time.Hour {
		t.Fatalf("crawl call = size %d cursor %q lookback %s", crawler.pageSize, crawler.cursor, crawler.lookback)
	}
	if len(st.ingest) != 3 {
		t.Fatalf("ingested = %d, want 3", len(st.ingest))
	}
	if resp.Source != api.SourceNyaa || resp.Posts != 3 || resp.PagesFetched != 2 ||
		resp.NextCursor != "next" || !resp.HasMore || resp.StopReason != api.CrawlStopPageBudget {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Batch.New != 3 || resp.Queue.Available != 12 {
		t.Fatalf("ingest/queue response = %+v", resp)
	}
	if resp.OldestPublishedAt == nil || !resp.OldestPublishedAt.Equal(older) {
		t.Fatalf("oldest = %v, want %v", resp.OldestPublishedAt, older)
	}
	if resp.NewestPublishedAt == nil || !resp.NewestPublishedAt.Equal(newer) {
		t.Fatalf("newest = %v, want %v", resp.NewestPublishedAt, newer)
	}
}

func TestCrawlEndpointDefaultsPageSize(t *testing.T) {
	crawler := &fakeChunkCrawler{result: crawl.CrawlResponse{
		StopReason: crawl.StopArchiveFloor,
	}}
	h := New(&fakeStore{})
	h.RegisterCrawler(api.SourceNyaa, crawler)

	rec := crawlRequest(t, h, api.SourceNyaa, `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if crawler.pageSize != 200 {
		t.Fatalf("pageSize = %d, want default 200", crawler.pageSize)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if raw["posts"] != float64(0) || raw["hasMore"] != false || raw["stopReason"] != api.CrawlStopArchiveFloor {
		t.Fatalf("response = %v", raw)
	}
	for _, key := range []string{"nextCursor", "oldestPublishedAt", "newestPublishedAt"} {
		if _, present := raw[key]; present {
			t.Fatalf("%s present on terminal empty chunk: %v", key, raw[key])
		}
	}
}

func TestCrawlEndpointRejectsBadRequests(t *testing.T) {
	h := New(&fakeStore{})
	h.RegisterCrawler(api.SourceNyaa, &fakeChunkCrawler{})

	tests := []struct {
		name   string
		source string
		body   string
		status int
		kind   string
	}{
		{name: "unknown source", source: "vhs", body: `{}`, status: http.StatusNotFound, kind: api.KindNotFound},
		{name: "unregistered source", source: "dmhy", body: `{}`, status: http.StatusNotFound, kind: api.KindNotFound},
		{name: "negative page size", source: "nyaa", body: `{"pageSize":-1}`, status: http.StatusBadRequest, kind: api.KindInvalidRequest},
		{name: "oversized page size", source: "nyaa", body: `{"pageSize":201}`, status: http.StatusBadRequest, kind: api.KindInvalidRequest},
		{name: "malformed cursor", source: "nyaa", body: `{"cursor":"nope"}`, status: http.StatusBadRequest, kind: api.KindInvalidRequest},
		{name: "foreign cursor", source: "nyaa", body: `{"cursor":"` + crawl.FormatCursor(api.SourceDMHY, 1, 0) + `"}`, status: http.StatusBadRequest, kind: api.KindInvalidRequest},
		{name: "malformed lookback", source: "nyaa", body: `{"lookback":"forever"}`, status: http.StatusBadRequest, kind: api.KindInvalidRequest},
		{name: "malformed body", source: "nyaa", body: `{`, status: http.StatusBadRequest, kind: api.KindInvalidRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := crawlRequest(t, h, tt.source, tt.body)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.status, rec.Body.String())
			}
			var envelope api.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode error envelope: %v", err)
			}
			if envelope.Kind != tt.kind {
				t.Fatalf("kind = %q, want %q", envelope.Kind, tt.kind)
			}
		})
	}
}

func TestCrawlEndpointMapsUpstreamFailureTo502(t *testing.T) {
	h := New(&fakeStore{})
	h.RegisterCrawler(api.SourceNyaa, &fakeChunkCrawler{err: errors.New("tls handshake timeout")})

	rec := crawlRequest(t, h, api.SourceNyaa, `{"pageSize":100}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var envelope api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Kind != api.KindUpstreamError {
		t.Fatalf("kind = %q, want %q", envelope.Kind, api.KindUpstreamError)
	}
}

func TestCrawlEndpointDoesNotReturnCursorWhenIngestFails(t *testing.T) {
	crawler := &fakeChunkCrawler{result: crawl.CrawlResponse{
		Posts: []api.RawPost{{
			Source:   api.SourceNyaa,
			SourceID: "1",
			Title:    "release",
			Magnet:   "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
		}},
		NextCursor: "must-not-escape",
		HasMore:    true,
		StopReason: crawl.StopPageBudget,
	}}
	h := New(&fakeStore{ingestErr: errors.New("database unavailable")})
	h.RegisterCrawler(api.SourceNyaa, crawler)

	rec := crawlRequest(t, h, api.SourceNyaa, `{"pageSize":1}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if raw["kind"] != api.KindInternal {
		t.Fatalf("response = %v", raw)
	}
	if _, ok := raw["nextCursor"]; ok {
		t.Fatalf("failed ingest exposed nextCursor: %v", raw)
	}
}

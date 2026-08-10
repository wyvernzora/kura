package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleAdd_RejectsBadMetadata(t *testing.T) {
	srv := newTestServer(t)
	body := strings.NewReader(`{"metadata":"bogus","ref":"foo"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/library/v1/series", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleImport_RequiresRef(t *testing.T) {
	srv := newTestServer(t)
	body := strings.NewReader(`{"metadata":"tvdb:123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/library/v1/series/import", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", rec.Code)
	}
}

func TestHandleReset_BadEpisode(t *testing.T) {
	srv := newTestServer(t)
	body := strings.NewReader(`{"episode":"bad-format"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/library/v1/series/foo/reset", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400, body=%s", rec.Code, rec.Body.String())
	}
}

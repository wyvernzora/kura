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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/series", body)
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/series/import", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", rec.Code)
	}
}

func TestHandleRemove_PurgeRequiresOperatorHeader(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/series/some-series?purge=1", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d want 403, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleReconcileRecover_RequiresOperatorHeader(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/series/some-series/reconcile/recover", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d want 403", rec.Code)
	}
}

func TestHandleReset_BadEpisode(t *testing.T) {
	srv := newTestServer(t)
	body := strings.NewReader(`{"episode":"bad-format"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/series/foo/reset", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400, body=%s", rec.Code, rec.Body.String())
	}
}

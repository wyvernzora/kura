package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleTrashEmptySeries_RequiresOperator(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/series/foo/trash", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d want 403", rec.Code)
	}
}

func TestHandleTrashEmptySeries_RequiresConfirm(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/series/foo/trash", http.NoBody)
	req.Header.Set(headerOperator, "1")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400 (missing X-Confirm), body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleTrashEmptyAll_RequiresOperator(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/trash", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d want 403", rec.Code)
	}
}

func TestHandleTrashRestore_RequiresOperator(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/series/foo/trash/01ARZ3NDEKTSV4RRFFQ69G5FAV/restore", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d want 403", rec.Code)
	}
}

func TestHandleTrashRestore_BadULID(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/series/foo/trash/notaulid/restore", http.NoBody)
	req.Header.Set(headerOperator, "1")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400, body=%s", rec.Code, rec.Body.String())
	}
}

// Reindex and ScanAll are long-running but non-destructive — neither
// is operator-gated. Both should accept ungated POSTs and return 202
// with a job handle. The operator gate test was removed when the gate
// was lifted; these replace it.

func TestHandleReindex_AcceptsUngated(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/library/reindex", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Errorf("status: got %d want 202, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleScanAll_AcceptsUngated(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/library/scan", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Errorf("status: got %d want 202, body=%s", rec.Code, rec.Body.String())
	}
}

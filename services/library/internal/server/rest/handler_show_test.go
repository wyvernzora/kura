package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleShow_NotFound(t *testing.T) {
	srv := newTestServer(t)
	// Valid metadata-ref shape, not in index.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/series/tvdb:999999999", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleShow_RejectsSeriesRefForm(t *testing.T) {
	srv := newTestServer(t)
	// Bare directory name = SeriesRef shape. Per Product.md
	// "Selectors, not paths," resource paths only accept metadata
	// refs. Server must reject with 400 invalid_ref.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/series/Frieren", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleLibrary_Returns200(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/library", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	var resp libraryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.LibraryRoot == "" {
		t.Errorf("libraryRoot empty")
	}
}

func TestHandleResolve_RejectsEmptyTerms(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/resolve", http.NoBody)
	req.Body = http.NoBody
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", rec.Code)
	}
}

func TestHandleTrashList_AllRoute_BadOlderThan(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trash?olderThan=banana", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", rec.Code)
	}
}

func TestHandleJobStatus_BadID(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/$bad$", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	// Pattern mismatch -> handler-level validation.
	// Accept either 400 (validation) or 404 (path not matched by mux).
	// Since path is `/api/v1/jobs/{job}` with permissive {job}, mux
	// should match and handler returns 400.
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d want 400 or 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleJobStatus_UnknownJob(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/abcdef0123456789", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d want 404, body=%s", rec.Code, rec.Body.String())
	}
}

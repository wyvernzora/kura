package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleAliasesList_UnknownRefIs404(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/series/tvdb:999999999/aliases", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleAliasesAdd_RejectsBadRef(t *testing.T) {
	srv := newTestServer(t)
	body := strings.NewReader(`{"aliases":["oreimo"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/series/Frieren/aliases", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	// "Frieren" parses as a non-metadata ref → 400.
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleAliasesRemove_UnknownRefIs404(t *testing.T) {
	srv := newTestServer(t)
	body := strings.NewReader(`{"aliases":["x"]}`)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/series/tvdb:999999999/aliases", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d want 404, body=%s", rec.Code, rec.Body.String())
	}
}

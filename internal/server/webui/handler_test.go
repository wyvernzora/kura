package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_ServesPlaceholderIndexAtRoot(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	if got, want := rec.Header().Get("Cache-Control"), "no-cache"; got != want {
		t.Errorf("Cache-Control: got %q want %q", got, want)
	}
	if !strings.Contains(rec.Body.String(), "<title>kura</title>") {
		t.Errorf("body missing kura title: %q", rec.Body.String())
	}
}

func TestHandler_FallsBackToIndexForUnknownPaths(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/series/some-ref", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	if got, want := rec.Header().Get("Cache-Control"), "no-cache"; got != want {
		t.Errorf("Cache-Control: got %q want %q", got, want)
	}
	if !strings.Contains(rec.Body.String(), "<title>kura</title>") {
		t.Errorf("body missing kura title: %q", rec.Body.String())
	}
}

func TestHandler_RejectsApiPrefix(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/should-not-exist", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d want 404", rec.Code)
	}
}

func TestHandler_RejectsNonGet(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d want 405", rec.Code)
	}
}

func TestHandler_HeadAtRootReturnsHeadersOnly(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodHead, "/series/x", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD body: got %d bytes, want empty", rec.Body.Len())
	}
}

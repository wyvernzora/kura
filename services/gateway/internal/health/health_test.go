package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func leafServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSystemAggregatesHealthyLeaves(t *testing.T) {
	lib := leafServer(t, 200, `{"ok":true,"version":"0.6.1"}`)
	rel := leafServer(t, 200, `{"ok":true,"version":"0.6.2"}`)

	h := New("9.9.9", []Leaf{
		{Name: "libraryManager", URL: lib.URL},
		{Name: "releaseIndexer", URL: rel.URL},
	}, time.Second)

	rec := httptest.NewRecorder()
	h.System(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	var got System
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != StatusOK || got.Version != "9.9.9" {
		t.Fatalf("system = %+v, want ok/9.9.9", got)
	}
	// The gateway reports itself without probing anything.
	if got.Components["gateway"].Status != StatusOK || got.Components["gateway"].Version != "9.9.9" {
		t.Errorf("gateway component = %+v", got.Components["gateway"])
	}
	if got.Components["libraryManager"].Version != "0.6.1" {
		t.Errorf("libraryManager = %+v, want version 0.6.1", got.Components["libraryManager"])
	}
	if got.Components["releaseIndexer"].Version != "0.6.2" {
		t.Errorf("releaseIndexer = %+v, want version 0.6.2", got.Components["releaseIndexer"])
	}
}

// A degraded leaf must make the whole response 503 — a 200 carrying
// status=degraded would let a probe treat the suite as healthy.
func TestSystemReportsDegradedWhenALeafIsDown(t *testing.T) {
	lib := leafServer(t, 200, `{"ok":true,"version":"0.6.1"}`)

	h := New("9.9.9", []Leaf{
		{Name: "libraryManager", URL: lib.URL},
		{Name: "releaseIndexer", URL: "http://127.0.0.1:1"},
	}, 200*time.Millisecond)

	rec := httptest.NewRecorder()
	h.System(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", http.NoBody))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", rec.Code, rec.Body.String())
	}
	var got System
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != StatusDegraded {
		t.Errorf("status = %q, want degraded", got.Status)
	}
	if got.Components["libraryManager"].Status != StatusOK {
		t.Errorf("healthy leaf should stay ok, got %+v", got.Components["libraryManager"])
	}
	if got.Components["releaseIndexer"].Status != StatusDegraded {
		t.Errorf("unreachable leaf = %+v, want degraded", got.Components["releaseIndexer"])
	}
}

// A leaf answering 200 with ok=false is degraded: the status line is not the
// contract, the body is.
func TestSystemTreatsOkFalseAsDegraded(t *testing.T) {
	lib := leafServer(t, 200, `{"ok":false,"version":"0.6.1"}`)
	h := New("9.9.9", []Leaf{{Name: "libraryManager", URL: lib.URL}}, time.Second)

	rec := httptest.NewRecorder()
	h.System(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", http.NoBody))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// /healthz must not depend on any leaf: it drives the Pod's liveness, and a
// database outage restarting the gateway would be a self-inflicted outage.
func TestLocalIgnoresLeafHealth(t *testing.T) {
	h := New("9.9.9", []Leaf{{Name: "libraryManager", URL: "http://127.0.0.1:1"}}, time.Second)

	rec := httptest.NewRecorder()
	h.Local(rec, httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got Local
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Ok || got.Version != "9.9.9" {
		t.Fatalf("local = %+v", got)
	}
}

// The probe must never echo the upstream failure: those carry hostnames and
// driver text that do not belong on a product endpoint.
func TestSystemDoesNotLeakUpstreamDetail(t *testing.T) {
	lib := leafServer(t, 500, `{"kind":"internal","message":"dial tcp 10.4.2.9:5432: connect: refused"}`)
	h := New("9.9.9", []Leaf{{Name: "libraryManager", URL: lib.URL}}, time.Second)

	rec := httptest.NewRecorder()
	h.System(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", http.NoBody))

	if body := rec.Body.String(); strings.Contains(body, "5432") || strings.Contains(body, "10.4.2.9") {
		t.Fatalf("health body leaked upstream detail: %s", body)
	}
}

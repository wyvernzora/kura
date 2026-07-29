package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("read scrape: %v", err)
	}
	return string(body)
}

func TestWrapHTTPRecordsRoutePattern(t *testing.T) {
	m := New("v-test")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/series/{ref}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := m.WrapHTTP(mux)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/series/tvdb:1", http.NoBody))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nowhere", http.NoBody))

	body := scrape(t, m)
	for _, want := range []string{
		`kura_library_build_info{version="v-test"} 1`,
		`kura_library_http_requests_total{method="GET",route="/api/v1/series/{ref}",status="418"} 1`,
		`kura_library_http_requests_total{method="GET",route="other",status="404"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape missing %q", want)
		}
	}
}

func TestJobHooksTrackRunningAndTerminal(t *testing.T) {
	m := New("v")
	m.JobStarted("scan")
	m.JobStarted("scan")
	m.JobTerminal("scan", "failed", 2*time.Second)

	body := scrape(t, m)
	for _, want := range []string{
		"kura_library_jobs_running 1",
		`kura_library_jobs_total{kind="scan",state="failed"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape missing %q", want)
		}
	}
}

func TestNilMetricsIsNoOp(t *testing.T) {
	var m *Metrics
	m.JobStarted("scan")
	m.JobTerminal("scan", "succeeded", time.Second)
	m.ObserveIndex(func() bool { return false }, func() int { return 0 })
	h := m.WrapHTTP(http.NotFoundHandler())
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", http.NoBody))
}

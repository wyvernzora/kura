package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	libRoot := t.TempDir()
	idx := indexfile.New(libRoot, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	registry := jobs.NewRegistry(context.Background(), jobs.Config{
		JobTimeout:     time.Hour,
		Retention:      time.Hour,
		ReaperInterval: time.Hour,
	}, nil)
	t.Cleanup(func() { registry.Shutdown(time.Second) })
	deps := workflow.Deps{
		LibRoot:     libRoot,
		Index:       idx,
		Coordinator: coord.NewCLICoordinator(),
		Now:         time.Now,
		Jobs:        registry,
	}
	return NewServer(Deps{Workflow: deps})
}

func TestHandleHealth_Returns200(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(headerVersion); got != serverVersion {
		t.Errorf("X-Kura-Version: got %q want %q", got, serverVersion)
	}
	var resp healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Ok {
		t.Errorf("ok: got false want true")
	}
	if resp.Version != serverVersion {
		t.Errorf("version: got %q want %q", resp.Version, serverVersion)
	}
}

func TestHandleList_EmptyLibrary(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/series", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get(headerETag) == "" {
		t.Errorf("ETag missing on list response")
	}
}

func TestHandleList_ETagShortCircuit(t *testing.T) {
	srv := newTestServer(t)

	// First request to capture ETag.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/series", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first status: got %d", rec.Code)
	}
	etag := rec.Header().Get(headerETag)
	if etag == "" {
		t.Fatal("etag empty on first response")
	}

	// Second request with If-None-Match should 304.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/series", http.NoBody)
	req2.Header.Set(headerIfNoneMatch, etag)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Errorf("status: got %d want 304", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Errorf("body should be empty on 304, got %d bytes", rec2.Body.Len())
	}
}

func TestHandleList_BadStatus(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/series?status=bogus", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400, body=%s", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(env.Message, "unknown status") {
		t.Errorf("message: got %q want contains 'unknown status'", env.Message)
	}
}

func TestHandleList_BadLimit(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/series?limit=-5", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", rec.Code)
	}
}

func TestHandleHealth_OptionsPreflightAnswered(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/health", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("OPTIONS status: got %d want 204", rec.Code)
	}
}

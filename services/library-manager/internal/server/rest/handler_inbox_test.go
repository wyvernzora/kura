package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

func newInboxTestServer(t *testing.T) (server *Server, inboxRoot string) {
	t.Helper()
	libRoot := t.TempDir()
	inboxRoot = t.TempDir()
	idx := indexfile.New(libRoot, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	registry := jobs.NewRegistry(context.Background(), jobs.Config{
		JobTimeout:     time.Hour,
		Retention:      time.Hour,
		ReaperInterval: time.Hour,
	}, nil)
	t.Cleanup(func() { registry.Shutdown(time.Second) })
	deps := workflow.Deps{
		LibRoot:     libRoot,
		InboxRoot:   inboxRoot,
		Index:       idx,
		Coordinator: coord.NewCLICoordinator(),
		Now:         time.Now,
		Jobs:        registry,
	}
	return NewServer(Deps{Workflow: deps}), inboxRoot
}

func writeInboxFile(t *testing.T, root, rel string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHandleInboxList_Empty(t *testing.T) {
	srv, _ := newInboxTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/inbox", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get(headerETag) == "" {
		t.Error("ETag header missing")
	}
	var resp api.InboxList
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Errorf("entries: got %d, want 0", len(resp.Entries))
	}
}

func TestHandleInboxList_ListsFiles(t *testing.T) {
	srv, root := newInboxTestServer(t)
	writeInboxFile(t, root, "a.mkv")
	writeInboxFile(t, root, "b.mkv")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/inbox", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp api.InboxList
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Entries) != 2 {
		t.Errorf("entries: got %d, want 2", len(resp.Entries))
	}
	for _, e := range resp.Entries {
		if e.Path == "" {
			t.Error("path empty")
		}
		if e.Kind != "file" {
			t.Errorf("kind: got %q, want file", e.Kind)
		}
	}
}

func TestHandleInboxList_BadDepth(t *testing.T) {
	srv, _ := newInboxTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/inbox?depth=-1", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rec.Code)
	}
}

func TestHandleInboxList_BadLimit(t *testing.T) {
	srv, _ := newInboxTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/inbox?limit=abc", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rec.Code)
	}
}

func TestHandleInboxList_LimitTooLarge(t *testing.T) {
	srv, _ := newInboxTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/inbox?limit=99999", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rec.Code)
	}
}

func TestHandleInboxList_PathNotFound(t *testing.T) {
	srv, _ := newInboxTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/inbox?path=missing", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleInboxList_Traversal(t *testing.T) {
	srv, _ := newInboxTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/inbox?path=../etc", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleInboxList_ETagShortCircuit(t *testing.T) {
	srv, root := newInboxTestServer(t)
	writeInboxFile(t, root, "a.mkv")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/inbox", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first status: got %d", rec.Code)
	}
	etag := rec.Header().Get(headerETag)
	if etag == "" {
		t.Fatal("etag empty")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/inbox", http.NoBody)
	req2.Header.Set(headerIfNoneMatch, etag)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Errorf("status: got %d, want 304", rec2.Code)
	}
}

func TestHandleInboxList_RecursiveQuery(t *testing.T) {
	srv, root := newInboxTestServer(t)
	writeInboxFile(t, root, "[BDrip] Show/E01.mkv")
	writeInboxFile(t, root, "[BDrip] Show/E02.mkv")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/inbox?recursive=1", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp api.InboxList
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Entries) < 3 {
		t.Errorf("recursive entries: got %d, want >= 3 (1 dir + 2 files)", len(resp.Entries))
	}
}

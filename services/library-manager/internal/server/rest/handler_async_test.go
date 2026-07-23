package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleApply_RequiresToken(t *testing.T) {
	srv := newTestServer(t)
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/series/foo/reconcile/apply", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleStage_RequiresAtLeastOneItem(t *testing.T) {
	srv := newTestServer(t)
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/series/foo/stage", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleStage_BadEpisodeRef(t *testing.T) {
	srv := newTestServer(t)
	body := strings.NewReader(`{"episodes":[{"episode":"oops","mediaPath":"/x"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/series/foo/stage", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestStageRequestToWorkflowCopiesAttrs(t *testing.T) {
	got := stageRequestToWorkflow(stageRequest{
		Episodes: []stageEpisode{{
			Episode: "S01E01",
			Media:   "inbox:ep1.mkv",
			Attrs:   map[string]string{"origin": "takuhai"},
		}},
	})
	if got.Episodes[0].Attrs["origin"] != "takuhai" {
		t.Fatalf("Attrs = %#v", got.Episodes[0].Attrs)
	}
}

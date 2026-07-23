package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/services/library/internal/coord"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/domain/series"
	"github.com/wyvernzora/kura/services/library/internal/response"
	"github.com/wyvernzora/kura/services/library/internal/storage/seriesfile"
)

func seedTaggedSeries(t *testing.T, srv *Server) {
	t.Helper()
	ref, err := refs.ParseSeries("Tagged Show")
	if err != nil {
		t.Fatal(err)
	}
	model := &series.Series{
		Ref:      ref,
		Metadata: refs.Metadata("tvdb:42"),
		Episodes: map[refs.Episode]series.Episode{},
		Tags:     []string{"maintenance-disabled"},
	}
	if err := seriesfile.SaveCAS(srv.deps.Workflow.LibRoot, model, coord.NewMutator("seed")); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	if err := srv.deps.Workflow.Index.SaveModel(context.Background(), model, coord.NewMutator("seed")); err != nil {
		t.Fatalf("SaveModel: %v", err)
	}
}

func TestHandleTagsUpdateMutatesAtomically(t *testing.T) {
	srv := newTestServer(t)
	seedTaggedSeries(t, srv)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/series/tvdb:42/tags", strings.NewReader(`{"tags":["Priority","!Maintenance-Disabled"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got response.SeriesTags
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.MetadataRef.String() != "tvdb:42" || len(got.Tags) != 1 || got.Tags[0] != "priority" {
		t.Fatalf("response = %+v", got)
	}
}

func TestHandleTagsUpdateRejectsInvalidExpression(t *testing.T) {
	srv := newTestServer(t)
	seedTaggedSeries(t, srv)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/series/tvdb:42/tags", strings.NewReader(`{"tags":["not valid"]}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleListFiltersSpaceDelimitedTags(t *testing.T) {
	srv := newTestServer(t)
	seedTaggedSeries(t, srv)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/series?tags=Maintenance-Disabled+%21PRIORITY", http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got response.ListResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got.Rows) != 1 || got.Rows[0].MetadataRef.String() != "tvdb:42" {
		t.Fatalf("rows = %+v", got.Rows)
	}
}

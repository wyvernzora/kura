package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/series"
	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
	"github.com/wyvernzora/kura/services/library-manager/internal/reconcile"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/textnorm"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

func connectInMemoryWithJobStatus(t *testing.T, deps Deps) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	addJobStatusTool(server, deps)
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, nil)
	st, ct := sdkmcp.NewInMemoryTransports()
	srvSession, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { srvSession.Close() })
	clientSession, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { clientSession.Close() })
	return clientSession
}

func TestKuraJobStatus_ToolIsRegistered(t *testing.T) {
	cs := connectInMemoryWithJobStatus(t, Deps{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "kura_job_status" {
		t.Fatalf("unexpected tool list: %+v", res.Tools)
	}
	if !res.Tools[0].Annotations.ReadOnlyHint {
		t.Fatal("ReadOnlyHint must be true")
	}
}

func TestKuraJobStatus_InvalidJobIDRejected(t *testing.T) {
	cs := connectInMemoryWithJobStatus(t, Deps{})
	// All non-ULID: empty, short, lowercase (Crockford base32 is uppercase),
	// 25-char (length wrong), 26 chars containing I (not in alphabet).
	for _, id := range []string{"", "short", "0123456789abcdef", "01HBCDEFGHJKMNPQRSTVWXYZ0", "01HBCDEFGHIJKMNPQRSTVWXYZ0"} {
		res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name:      "kura_job_status",
			Arguments: map[string]any{"jobId": id},
		})
		if err != nil {
			t.Fatalf("CallTool(%q): %v", id, err)
		}
		if !res.IsError {
			t.Fatalf("id=%q IsError = false, want true", id)
		}
		body, err := decodeStructured(res.StructuredContent)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["kind"] != errkind.KindInvalidRef {
			t.Fatalf("id=%q kind = %v, want %v", id, body["kind"], errkind.KindInvalidRef)
		}
	}
}

func TestKuraJobStatus_NotFoundReturnsCodedError(t *testing.T) {
	registry := jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { registry.Shutdown(time.Second) })
	deps := Deps{Workflow: workflow.Deps{Jobs: registry}}
	cs := connectInMemoryWithJobStatus(t, deps)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "kura_job_status",
		Arguments: map[string]any{"jobId": "00000000000000000000000000"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true")
	}
	body, err := decodeStructured(res.StructuredContent)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["kind"] != errkind.KindNotFound {
		t.Fatalf("kind = %v, want %v", body["kind"], errkind.KindNotFound)
	}
}

func TestProjectJobStatus_ReversesSeriesToMetadataRef(t *testing.T) {
	registry := jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { registry.Shutdown(time.Second) })
	idx := indexfile.New(t.TempDir(), indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	want := mustSeries(t, "Bookworm")
	if err := idx.Upsert(indexfile.Entry{Model: &series.Series{
		Ref:            want,
		Metadata:       refs.Metadata("tvdb:370070"),
		PreferredTitle: textnorm.NFC("Bookworm"),
		Episodes:       map[refs.Episode]series.Episode{},
		LastMutated:    coord.Mutator{Op: "test", PID: 1, Host: "test", At: time.Unix(0, 0).UTC()},
	}}); err != nil {
		t.Fatal(err)
	}
	finish := make(chan struct{})
	j := jobs.Submit(registry, context.Background(), jobs.KindScan, want, func(_ context.Context) (int, error) {
		<-finish
		return 0, nil
	})
	view, err := registry.Get(j.ID())
	if err != nil {
		t.Fatal(err)
	}
	out, err := projectJobStatus(view, idx, false)
	if err != nil {
		t.Fatal(err)
	}
	close(finish)
	j.Wait(context.Background())

	if out.MetadataRef != "tvdb:370070" {
		t.Fatalf("MetadataRef = %q, want tvdb:370070", out.MetadataRef)
	}
	if out.Kind != "scan" {
		t.Fatalf("Kind = %q, want scan", out.Kind)
	}
	if out.State != "running" {
		t.Fatalf("State = %q, want running", out.State)
	}
}

func TestProjectJobError_CodedErrorPreservesKindAndData(t *testing.T) {
	got := projectJobError(&jobs.JobBusyError{
		Series:   mustSeries(t, "Show"),
		Existing: jobs.BusyHandle{JobID: "abc", Kind: jobs.KindScan, Series: mustSeries(t, "Show")},
	}, nil)
	if got.Kind != errkind.KindBusy {
		t.Fatalf("Kind = %q, want busy", got.Kind)
	}
	if got.Category != errkind.CategoryInternalError {
		t.Fatalf("Category = %q, want %q", got.Category, errkind.CategoryInternalError)
	}
	if got.Data == nil || got.Data["series"] != "Show" {
		t.Fatalf("Data = %+v, want series=Show", got.Data)
	}
}

func TestProjectJobError_UnknownErrorFallsBackToInternal(t *testing.T) {
	got := projectJobError(errors.New("anything"), nil)
	if got.Kind != errkind.KindInternal {
		t.Fatalf("Kind = %q, want internal", got.Kind)
	}
	if got.Category != errkind.CategoryInternalError {
		t.Fatalf("Category = %q, want %q", got.Category, errkind.CategoryInternalError)
	}
	if got.Message != "anything" {
		t.Fatalf("Message = %q, want anything", got.Message)
	}
}

func TestProjectJobStatus_ResultRequiresIncludeResult(t *testing.T) {
	registry := jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { registry.Shutdown(time.Second) })
	j := jobs.Submit(registry, context.Background(), jobs.KindScan, mustSeries(t, "Show"), func(context.Context) (map[string]int, error) {
		return map[string]int{"synced": 3}, nil
	})
	if _, err := j.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	view, err := registry.Get(j.ID())
	if err != nil {
		t.Fatal(err)
	}

	compact, err := projectJobStatus(view, indexfile.New(t.TempDir(), indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()}), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(compact.Result) != 0 {
		t.Fatalf("compact Result = %s, want omitted", compact.Result)
	}

	full, err := projectJobStatus(view, indexfile.New(t.TempDir(), indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()}), true)
	if err != nil {
		t.Fatal(err)
	}
	if string(full.Result) != `{"synced":3}` {
		t.Fatalf("full Result = %s, want synced payload", full.Result)
	}
}

func TestProjectJobStatus_ProjectsReconcileApplySuccess(t *testing.T) {
	registry := jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { registry.Shutdown(time.Second) })
	j := jobs.Submit(registry, context.Background(), jobs.KindReconcileApply, mustSeries(t, "Show"), func(context.Context) (reconcile.ApplyResult, error) {
		return reconcile.ApplyResult{
			Series:         mustSeries(t, "Show"),
			AppliedSteps:   1,
			TotalSteps:     2,
			AppliedStepIDs: []string{"step1"},
		}, nil
	})
	if _, err := j.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	view, err := registry.Get(j.ID())
	if err != nil {
		t.Fatal(err)
	}
	out, err := projectJobStatus(view, indexfile.New(t.TempDir(), indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()}), true)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{}
	if err := json.Unmarshal(out.Result, &body); err != nil {
		t.Fatal(err)
	}
	if body["series"] != "Show" || body["appliedSteps"] != float64(1) || body["totalSteps"] != float64(2) {
		t.Fatalf("projected reconcile result = %s", out.Result)
	}
}

func TestProjectJobStatus_FailedReconcileApplyIncludesCategoryAndPartialResult(t *testing.T) {
	registry := jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { registry.Shutdown(time.Second) })
	stepErr := &reconcile.ApplyStepError{
		StepID:    "step1",
		StepKind:  reconcile.StepFileMove,
		OwnerKind: reconcile.OwnerEpisode,
		From:      "inbox:release.mkv",
		To:        "Season 1/Show - S01E01.mkv",
		Err:       os.ErrNotExist,
	}
	j := jobs.Submit(registry, context.Background(), jobs.KindReconcileApply, mustSeries(t, "Show"), func(context.Context) (reconcile.ApplyResult, error) {
		return reconcile.ApplyResult{
			Series:         mustSeries(t, "Show"),
			AppliedSteps:   1,
			TotalSteps:     2,
			AppliedStepIDs: []string{"step0"},
			FailedStep: &reconcile.FailedStepRef{
				ID:         "step1",
				Kind:       reconcile.StepFileMove,
				OwnerKind:  reconcile.OwnerEpisode,
				From:       "inbox:release.mkv",
				To:         "Season 1/Show - S01E01.mkv",
				ErrMessage: stepErr.Error(),
			},
		}, stepErr
	})
	if _, err := j.Wait(context.Background()); err == nil {
		t.Fatal("Wait error = nil, want failure")
	}
	view, err := registry.Get(j.ID())
	if err != nil {
		t.Fatal(err)
	}
	out, err := projectJobStatus(view, indexfile.New(t.TempDir(), indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()}), false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Error == nil {
		t.Fatal("Error = nil")
	}
	if out.Error.Kind != errkind.KindApplyStepFailed {
		t.Fatalf("Error.Kind = %q, want %q", out.Error.Kind, errkind.KindApplyStepFailed)
	}
	if out.Error.Category != errkind.CategoryInternalError {
		t.Fatalf("Error.Category = %q, want %q", out.Error.Category, errkind.CategoryInternalError)
	}
	result, ok := out.Error.Data["result"].(api.ReconcileApply)
	if !ok {
		t.Fatalf("error.data.result = %T, want api.ReconcileApply", out.Error.Data["result"])
	}
	if result.AppliedSteps != 1 || result.TotalSteps != 2 || result.FailedStep == nil {
		t.Fatalf("partial result = %+v", result)
	}
}

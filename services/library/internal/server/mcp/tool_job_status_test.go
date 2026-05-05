package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/workflow"
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
	for _, id := range []string{"", "short", "not-hex-just-text", "0123456789abcdefg" /*17*/} {
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
		Arguments: map[string]any{"jobId": "0123456789abcdef"},
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
	idx := indexfile.New(t.TempDir())
	want := mustSeries(t, "Bookworm")
	if err := idx.Put(refs.Metadata("tvdb:370070"), want); err != nil {
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
	out := projectJobStatus(view, idx)
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
	})
	if got.Kind != errkind.KindBusy {
		t.Fatalf("Kind = %q, want busy", got.Kind)
	}
	if got.Data == nil || got.Data["series"] != "Show" {
		t.Fatalf("Data = %+v, want series=Show", got.Data)
	}
}

func TestProjectJobError_UnknownErrorFallsBackToInternal(t *testing.T) {
	got := projectJobError(errors.New("anything"))
	if got.Kind != errkind.KindInternal {
		t.Fatalf("Kind = %q, want internal", got.Kind)
	}
	if got.Message != "anything" {
		t.Fatalf("Message = %q, want anything", got.Message)
	}
}

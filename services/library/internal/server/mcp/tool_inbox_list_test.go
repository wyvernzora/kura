package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/workflow"
)

func connectInMemoryWithInboxList(t *testing.T, inboxRoot string) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	libRoot := t.TempDir()
	idx := indexfile.New(libRoot, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	registry := jobs.NewRegistry(ctx, jobs.Config{
		JobTimeout:     time.Hour,
		Retention:      time.Hour,
		ReaperInterval: time.Hour,
	}, nil)
	t.Cleanup(func() { registry.Shutdown(time.Second) })
	deps := Deps{Workflow: workflow.Deps{
		LibRoot:     libRoot,
		InboxRoot:   inboxRoot,
		Index:       idx,
		Coordinator: coord.NewCLICoordinator(),
		Now:         time.Now,
		Jobs:        registry,
	}}

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	addInboxListTool(server, deps)

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

func TestKuraInboxList_ToolIsRegistered(t *testing.T) {
	cs := connectInMemoryWithInboxList(t, t.TempDir())
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(res.Tools))
	}
	if res.Tools[0].Name != "kura_inbox_list" {
		t.Errorf("Name = %q, want kura_inbox_list", res.Tools[0].Name)
	}
}

func TestKuraInboxList_ReturnsStructuredContentOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "alpha.mkv"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := connectInMemoryWithInboxList(t, root)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "kura_inbox_list",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError, content=%v structured=%v", res.Content, res.StructuredContent)
	}
	if len(res.Content) != 0 {
		t.Fatalf("Content len = %d, want 0", len(res.Content))
	}
	body, err := decodeStructured(res.StructuredContent)
	if err != nil {
		t.Fatalf("decode StructuredContent: %v", err)
	}
	entries, ok := body["entries"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("entries = %#v, want one entry", body["entries"])
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("entry = %T, want map", entries[0])
	}
	if entry["path"] != "inbox:alpha.mkv" {
		t.Fatalf("path = %v, want inbox:alpha.mkv", entry["path"])
	}
	if entry["kind"] != "file" {
		t.Fatalf("kind = %v, want file", entry["kind"])
	}
}

func TestKuraInboxList_RejectsBadKind(t *testing.T) {
	cs := connectInMemoryWithInboxList(t, t.TempDir())
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "kura_inbox_list",
		Arguments: map[string]any{"kind": "weird"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
	body, err := decodeStructured(res.StructuredContent)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["kind"] != errkind.KindInvalidRef {
		t.Errorf("kind = %v, want invalid_ref", body["kind"])
	}
}

func TestKuraInboxList_NotConfigured(t *testing.T) {
	// Empty inbox root → InboxNotConfiguredError surfaces.
	ctx := context.Background()
	libRoot := t.TempDir()
	idx := indexfile.New(libRoot, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	registry := jobs.NewRegistry(ctx, jobs.Config{
		JobTimeout:     time.Hour,
		Retention:      time.Hour,
		ReaperInterval: time.Hour,
	}, nil)
	t.Cleanup(func() { registry.Shutdown(time.Second) })
	deps := Deps{Workflow: workflow.Deps{
		LibRoot:     libRoot,
		Index:       idx,
		Coordinator: coord.NewCLICoordinator(),
		Now:         time.Now,
		Jobs:        registry,
	}}

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	addInboxListTool(server, deps)
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

	res, err := clientSession.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "kura_inbox_list",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
}

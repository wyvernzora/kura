package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/errkind"
)

func connectInMemoryWithAdd(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	addAddTool(server, Deps{})
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

func TestKuraAdd_ToolIsRegistered(t *testing.T) {
	cs := connectInMemoryWithAdd(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "kura_add" {
		t.Fatalf("unexpected tool list: %+v", res.Tools)
	}
	got := res.Tools[0]
	if got.Annotations == nil || got.Annotations.ReadOnlyHint {
		t.Fatal("ReadOnlyHint must be false (mutating)")
	}
	if got.Annotations.OpenWorldHint == nil || !*got.Annotations.OpenWorldHint {
		t.Fatal("OpenWorldHint must be true (provider fetch)")
	}
}

func TestKuraAdd_InvalidRefRejected(t *testing.T) {
	cs := connectInMemoryWithAdd(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "kura_add",
		Arguments: map[string]any{"ref": "not-a-ref"},
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
	if body["kind"] != errkind.KindInvalidRef {
		t.Fatalf("kind = %v, want %v", body["kind"], errkind.KindInvalidRef)
	}
}

func TestKuraAdd_InvalidDirnameRejected(t *testing.T) {
	cs := connectInMemoryWithAdd(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "kura_add",
		Arguments: map[string]any{
			"ref":     "tvdb:1",
			"dirname": "bad/with/slashes",
		},
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
	if body["kind"] != errkind.KindInvalidRef {
		t.Fatalf("kind = %v, want %v", body["kind"], errkind.KindInvalidRef)
	}
}

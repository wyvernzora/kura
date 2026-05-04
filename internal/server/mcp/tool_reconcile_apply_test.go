package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/errkind"
)

func connectInMemoryWithReconcileApply(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	addReconcileApplyTool(server, Deps{})
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

func TestKuraReconcileApply_ToolIsRegistered(t *testing.T) {
	cs := connectInMemoryWithReconcileApply(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "kura_reconcile_apply" {
		t.Fatalf("unexpected tool list: %+v", res.Tools)
	}
	got := res.Tools[0]
	if got.Annotations.DestructiveHint == nil || !*got.Annotations.DestructiveHint {
		t.Fatal("DestructiveHint must be true (file moves + trash)")
	}
}

func TestKuraReconcileApply_InvalidRefRejected(t *testing.T) {
	cs := connectInMemoryWithReconcileApply(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "kura_reconcile_apply",
		Arguments: map[string]any{
			"ref":   "not-a-ref",
			"token": "abc123def456",
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

func TestKuraReconcileApply_InvalidTokenRejected(t *testing.T) {
	cs := connectInMemoryWithReconcileApply(t)
	cases := []string{"", "short", "ZZZZZZZZZZZZ", "abc123def4567" /* 13 chars */}
	for _, tok := range cases {
		res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name:      "kura_reconcile_apply",
			Arguments: map[string]any{"ref": "tvdb:1", "token": tok},
		})
		if err != nil {
			t.Fatalf("CallTool(token=%q): %v", tok, err)
		}
		if !res.IsError {
			t.Fatalf("token=%q IsError = false, want true", tok)
		}
		body, err := decodeStructured(res.StructuredContent)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["kind"] != errkind.KindInvalidRef {
			t.Fatalf("token=%q kind = %v, want %v", tok, body["kind"], errkind.KindInvalidRef)
		}
	}
}

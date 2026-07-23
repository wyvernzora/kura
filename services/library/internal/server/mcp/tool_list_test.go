package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library/internal/errkind"
)

func connectInMemoryWithList(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	addListTool(server, Deps{})
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

func TestKuraList_ToolIsRegistered(t *testing.T) {
	cs := connectInMemoryWithList(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(res.Tools))
	}
	got := res.Tools[0]
	if got.Name != "kura_list" {
		t.Fatalf("Name = %q, want kura_list", got.Name)
	}
	if got.Annotations == nil || got.Annotations.OpenWorldHint == nil || *got.Annotations.OpenWorldHint {
		t.Fatal("OpenWorldHint must be false (no external calls)")
	}
}

func TestKuraList_UnknownStatusRejected(t *testing.T) {
	cs := connectInMemoryWithList(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "kura_list",
		Arguments: map[string]any{"statuses": []string{"bogus"}},
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

func TestKuraList_UntrackedAccepted(t *testing.T) {
	got, err := parseListStatuses([]string{"untracked"})
	if err != nil {
		t.Fatalf("parseListStatuses: %v", err)
	}
	if len(got) != 1 || string(got[0]) != "untracked" {
		t.Fatalf("got = %v, want [untracked]", got)
	}
}

func TestKuraList_EmptyReturnsNil(t *testing.T) {
	got, err := parseListStatuses(nil)
	if err != nil || got != nil {
		t.Fatalf("parseListStatuses(nil) = (%v, %v); want (nil, nil)", got, err)
	}
}

func TestKuraList_NegativeMaxResultsRejected(t *testing.T) {
	cs := connectInMemoryWithList(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "kura_list",
		Arguments: map[string]any{"maxResults": -1},
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

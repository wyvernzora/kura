package mcp

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library/internal/errkind"
)

// connectInMemory wires a server-side mcp.Server to a client session
// via the SDK's in-memory transport. The server has the resolve tool
// registered with empty workflow.Deps; only schema- and handler-level
// behavior (no real provider call) is exercised here.
func connectInMemory(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	addResolveTool(server, Deps{})
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

func TestKuraResolve_ToolIsRegistered(t *testing.T) {
	cs := connectInMemory(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(res.Tools))
	}
	got := res.Tools[0]
	if got.Name != "kura_resolve" {
		t.Fatalf("Name = %q, want kura_resolve", got.Name)
	}
	if got.Description == "" {
		t.Fatal("Description empty")
	}
	if got.Annotations == nil || !got.Annotations.ReadOnlyHint {
		t.Fatal("ReadOnlyHint not set")
	}
}

func TestKuraResolve_EmptyTermsReturnsInvalidRef(t *testing.T) {
	cs := connectInMemory(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "kura_resolve",
		Arguments: map[string]any{"terms": []string{}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true")
	}
	body, err := decodeStructured(res.StructuredContent)
	if err != nil {
		t.Fatalf("decode StructuredContent: %v", err)
	}
	if body["kind"] != errkind.KindInvalidRef {
		t.Fatalf("kind = %v, want %v", body["kind"], errkind.KindInvalidRef)
	}
	if body["category"] != errkind.CategoryInvalidParams {
		t.Fatalf("category = %v, want %v", body["category"], errkind.CategoryInvalidParams)
	}
}

// decodeStructured handles the case where StructuredContent comes back
// as json.RawMessage from the wire roundtrip vs. a map[string]any
// when accessed in-process.
func decodeStructured(v any) (map[string]any, error) {
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

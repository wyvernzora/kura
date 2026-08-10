package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/gateway/internal/client"
)

func testServer(t *testing.T) *mcpsdk.ClientSession {
	t.Helper()
	opts := client.Options{RequestTimeout: time.Second, MaxResponseBytes: 1 << 20}
	s := New("test", client.New("127.0.0.1:1", "/api/library/v1", opts), client.New("127.0.0.1:1", "/api/releases/v1", opts), nil)

	ct, st := mcpsdk.NewInMemoryTransports()
	go func() { _, _ = s.sdk.Connect(context.Background(), st, nil) }()
	sess, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func TestHandlerExpiresIdleSessions(t *testing.T) {
	opts := client.Options{RequestTimeout: time.Second, MaxResponseBytes: 1 << 20}
	s := New("test", client.New("127.0.0.1:1", "/api/library/v1", opts), client.New("127.0.0.1:1", "/api/releases/v1", opts), nil)
	httpServer := httptest.NewServer(s.Handler(50 * time.Millisecond))
	t.Cleanup(httpServer.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	sess, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test"}, nil).Connect(
		ctx,
		&mcpsdk.StreamableClientTransport{Endpoint: httpServer.URL},
		nil,
	)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	if got := len(slices.Collect(s.sdk.Sessions())); got != 1 {
		t.Fatalf("active sessions after connect = %d, want 1", got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(slices.Collect(s.sdk.Sessions())) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("idle MCP session did not expire")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// wantTools is the catalog, in the plan's order. The count is asserted
// exactly: with the operator tier gone, this list is the only thing keeping
// destructive operations off the MCP surface, so an addition must fail here
// rather than ship quietly.
var wantTools = []string{
	"resolve_series",
	"list_series",
	"get_series",
	"update_series_tags",
	"add_series",
	"import_series",
	"scan_series",
	"stage_series_media",
	"reset_series_staging",
	"plan_series_reconcile",
	"apply_series_reconcile",
	"get_job",
	"list_inbox",
	"list_releases",
	"get_release",
	"get_magnet",
}

func TestCatalogIsExactlyTheSixteenTools(t *testing.T) {
	sess := testServer(t)
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}

	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	if len(res.Tools) != len(wantTools) {
		t.Errorf("tool count = %d, want %d", len(res.Tools), len(wantTools))
	}
	for _, name := range wantTools {
		if !got[name] {
			t.Errorf("missing tool %q", name)
		}
		delete(got, name)
	}
	for extra := range got {
		t.Errorf("unexpected tool %q — §5.2 keeps destructive operations off this surface", extra)
	}
}

// The leaf-era names must not survive anywhere an agent can read them.
func TestNoLeafEraToolNamesInDescriptionsOrInstructions(t *testing.T) {
	sess := testServer(t)
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	for _, tool := range res.Tools {
		if strings.Contains(tool.Name, "kura_") {
			t.Errorf("tool name %q carries a leaf-era name", tool.Name)
		}
		if strings.Contains(tool.Description, "kura_") {
			t.Errorf("tool %q description mentions a leaf-era name", tool.Name)
		}
	}
	if strings.Contains(instructions, "kura_") {
		t.Error("server instructions mention a leaf-era name")
	}
}

func TestAnnotationsMatchThePlan(t *testing.T) {
	sess := testServer(t)
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	byName := map[string]*mcpsdk.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}

	// R/I/D/O per the plan's annotation columns.
	for _, tc := range []struct {
		name                          string
		read, idem, destructive, open bool
	}{
		{"resolve_series", true, true, false, true},
		{"list_series", true, true, false, false},
		{"get_series", true, true, false, false},
		{"update_series_tags", false, true, false, false},
		{"add_series", false, false, false, true},
		{"import_series", false, false, false, true},
		{"scan_series", false, false, false, true},
		{"stage_series_media", false, false, false, false},
		{"reset_series_staging", false, true, true, false},
		{"plan_series_reconcile", false, true, false, false},
		{"apply_series_reconcile", false, true, true, false},
		{"get_job", true, true, false, false},
		{"list_inbox", true, true, false, false},
		{"list_releases", true, true, false, false},
		{"get_release", true, true, false, false},
		{"get_magnet", true, true, false, false},
	} {
		tool, ok := byName[tc.name]
		if !ok {
			t.Errorf("%s: not registered", tc.name)
			continue
		}
		a := tool.Annotations
		if a == nil {
			t.Errorf("%s: no annotations", tc.name)
			continue
		}
		if a.ReadOnlyHint != tc.read {
			t.Errorf("%s: readOnly = %v, want %v", tc.name, a.ReadOnlyHint, tc.read)
		}
		if a.IdempotentHint != tc.idem {
			t.Errorf("%s: idempotent = %v, want %v", tc.name, a.IdempotentHint, tc.idem)
		}
		if a.DestructiveHint == nil || *a.DestructiveHint != tc.destructive {
			t.Errorf("%s: destructive = %v, want %v", tc.name, a.DestructiveHint, tc.destructive)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint != tc.open {
			t.Errorf("%s: openWorld = %v, want %v", tc.name, a.OpenWorldHint, tc.open)
		}
	}
}

// A leaf being down changes results, never the catalog: an agent that cannot
// see a tool concludes the capability does not exist.
func TestBackendOutageDoesNotAlterTheCatalog(t *testing.T) {
	sess := testServer(t) // both clients point at a closed port
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(res.Tools) != len(wantTools) {
		t.Fatalf("tool count with backends down = %d, want %d", len(res.Tools), len(wantTools))
	}

	// And a call against a dead backend is a tool result, not a protocol
	// error, carrying backend_unavailable and no structuredContent.
	out, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "list_series",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call returned a protocol error, want a tool result: %v", err)
	}
	if !out.IsError {
		t.Fatal("call against a dead backend succeeded")
	}
	if out.StructuredContent != nil {
		t.Errorf("error result carried structuredContent: %#v", out.StructuredContent)
	}
	text := firstText(out)
	var env client.Error
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("error content is not the envelope: %s", text)
	}
	if env.Kind != client.KindBackendUnavailable {
		t.Errorf("kind = %q, want %q", env.Kind, client.KindBackendUnavailable)
	}
}

func firstText(res *mcpsdk.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

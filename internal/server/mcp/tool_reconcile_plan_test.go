package mcp

import (
	"context"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/response"
)

func connectInMemoryWithReconcilePlan(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	addReconcilePlanTool(server, Deps{})
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

func TestKuraReconcilePlan_ToolIsRegistered(t *testing.T) {
	cs := connectInMemoryWithReconcilePlan(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "kura_reconcile_plan" {
		t.Fatalf("unexpected tool list: %+v", res.Tools)
	}
	if !res.Tools[0].Annotations.IdempotentHint {
		t.Fatal("IdempotentHint must be true")
	}
}

func TestKuraReconcilePlan_InvalidRefRejected(t *testing.T) {
	cs := connectInMemoryWithReconcilePlan(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "kura_reconcile_plan",
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

func TestRelativizeUnderRoot(t *testing.T) {
	cases := []struct {
		path, root, want string
	}{
		{"/lib/Show/Season 1/x.mkv", "/lib/Show", "Season 1/x.mkv"},
		{"/external/staged.mkv", "/lib/Show", "/external/staged.mkv"},
		{"Season 1/x.mkv", "/lib/Show", "Season 1/x.mkv"}, // already relative
		{"", "/lib/Show", ""},
		{"/lib/ShowOther/x.mkv", "/lib/Show", "/lib/ShowOther/x.mkv"}, // prefix collision
	}
	for _, tc := range cases {
		got := relativizeUnderRoot(tc.path, tc.root)
		if got != tc.want {
			t.Errorf("relativizeUnderRoot(%q, %q) = %q, want %q", tc.path, tc.root, got, tc.want)
		}
	}
}

func TestProjectReconcilePlan_DropsCreatedAtAndPlanWrapper(t *testing.T) {
	expires := time.Date(2026, 5, 4, 0, 30, 0, 0, time.UTC)
	in := response.ReconcilePlan{
		Token:     "abc123",
		ExpiresAt: &expires,
		Plan: response.ReconcilePlanDetail{
			Series: mustSeries(t, "Show"),
			Changes: []response.ReconcileChange{
				{
					Kind:       "add",
					Episode:    mustEpisode(t, 1, 1),
					From:       "/lib/Show/Season 1/x.mkv",
					To:         "/lib/Show/Season 1/x (BluRay 1080p).mkv",
					Source:     "BluRay",
					Resolution: "1080p",
				},
			},
		},
	}
	out := projectReconcilePlan(in, "/lib/Show")
	if out.Token != "abc123" {
		t.Fatalf("Token = %q, want abc123", out.Token)
	}
	if out.ExpiresAt == "" {
		t.Fatal("ExpiresAt empty")
	}
	if len(out.Changes) != 1 {
		t.Fatalf("Changes = %v", out.Changes)
	}
	got := out.Changes[0]
	if got.From != "Season 1/x.mkv" {
		t.Fatalf("From = %q, want Season 1/x.mkv", got.From)
	}
	if got.To != "Season 1/x (BluRay 1080p).mkv" {
		t.Fatalf("To = %q, want relative", got.To)
	}
}

func TestProjectReconcilePlan_DropsReplacedTo(t *testing.T) {
	in := response.ReconcilePlan{
		Token: "tok",
		Plan: response.ReconcilePlanDetail{
			Series: mustSeries(t, "Show"),
			Changes: []response.ReconcileChange{
				{
					Kind:    "replace",
					Episode: mustEpisode(t, 1, 1),
					From:    "/lib/Show/staged.mkv",
					To:      "/lib/Show/Season 1/x.mkv",
					Replaced: &response.ReconcileReplaced{
						From:       "/lib/Show/Season 1/old.mkv",
						To:         "/lib/Show/.kura/trash/01/old.mkv",
						Source:     "WebRip",
						Resolution: "720p",
					},
				},
			},
		},
	}
	out := projectReconcilePlan(in, "/lib/Show")
	rep := out.Changes[0].Replaced
	if rep == nil {
		t.Fatal("Replaced is nil")
	}
	if rep.From != "Season 1/old.mkv" {
		t.Fatalf("Replaced.From = %q, want Season 1/old.mkv", rep.From)
	}
	if rep.Source != "WebRip" {
		t.Fatalf("Replaced.Source = %q", rep.Source)
	}
}

func TestProjectReconcilePlan_ExternalFromStaysAbsolute(t *testing.T) {
	in := response.ReconcilePlan{
		Token: "tok",
		Plan: response.ReconcilePlanDetail{
			Series: mustSeries(t, "Show"),
			Changes: []response.ReconcileChange{
				{
					Kind:    "add",
					Episode: mustEpisode(t, 1, 1),
					From:    "/external/inbox/x.mkv",
					To:      "/lib/Show/Season 1/x.mkv",
				},
			},
		},
	}
	out := projectReconcilePlan(in, "/lib/Show")
	got := out.Changes[0]
	if got.From != "/external/inbox/x.mkv" {
		t.Fatalf("From = %q, want absolute external path", got.From)
	}
	if got.To != "Season 1/x.mkv" {
		t.Fatalf("To = %q, want relative", got.To)
	}
}

// silence unused-helper lint when run alone.
var _ = refs.Episode{}

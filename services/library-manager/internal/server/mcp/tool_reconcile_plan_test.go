package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
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

func TestProjectReconcilePreviewPath(t *testing.T) {
	cases := []struct {
		path, root, want string
	}{
		{"/lib/Show/Season 1/x.mkv", "/lib/Show", "Season 1/x.mkv"},
		{"/external/staged.mkv", "/lib/Show", "staged.mkv"},
		{"Season 1/x.mkv", "/lib/Show", "Season 1/x.mkv"}, // already relative
		{"", "/lib/Show", ""},
		{"/lib/ShowOther/x.mkv", "/lib/Show", "x.mkv"}, // prefix collision
		{"/inbox/release/x.mkv", "/lib/Show", "inbox:release/x.mkv"},
	}
	for _, tc := range cases {
		got := projectReconcilePreviewPath(tc.path, tc.root, "/inbox")
		if got != tc.want {
			t.Errorf("projectReconcilePreviewPath(%q, %q, /inbox) = %q, want %q", tc.path, tc.root, got, tc.want)
		}
	}
}

func TestProjectReconcilePlan_DropsCreatedAtAndPlanWrapper(t *testing.T) {
	in := api.ReconcilePlan{
		Token: "abc123",
		Plan: api.ReconcilePlanDetail{
			Series: mustSeries(t, "Show"),
			Changes: []api.ReconcileChange{
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
	out := projectReconcilePlan(in, "/lib/Show", "/inbox")
	if out.Token != "abc123" {
		t.Fatalf("Token = %q, want abc123", out.Token)
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
	in := api.ReconcilePlan{
		Token: "tok",
		Plan: api.ReconcilePlanDetail{
			Series: mustSeries(t, "Show"),
			Changes: []api.ReconcileChange{
				{
					Kind:    "replace",
					Episode: mustEpisode(t, 1, 1),
					From:    "/lib/Show/staged.mkv",
					To:      "/lib/Show/Season 1/x.mkv",
					Replaced: &api.ReconcileReplaced{
						From:       "/lib/Show/Season 1/old.mkv",
						To:         "/lib/Show/.kura/trash/01/old.mkv",
						Source:     "WebRip",
						Resolution: "720p",
					},
				},
			},
		},
	}
	out := projectReconcilePlan(in, "/lib/Show", "/inbox")
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

func TestProjectReconcilePlan_ExternalFromDoesNotLeakAbsolutePath(t *testing.T) {
	in := api.ReconcilePlan{
		Token: "tok",
		Plan: api.ReconcilePlanDetail{
			Series: mustSeries(t, "Show"),
			Changes: []api.ReconcileChange{
				{
					Kind:    "add",
					Episode: mustEpisode(t, 1, 1),
					From:    "/external/inbox/x.mkv",
					To:      "/lib/Show/Season 1/x.mkv",
				},
			},
		},
	}
	out := projectReconcilePlan(in, "/lib/Show", "/inbox")
	got := out.Changes[0]
	if got.From != "x.mkv" {
		t.Fatalf("From = %q, want basename", got.From)
	}
	if got.To != "Season 1/x.mkv" {
		t.Fatalf("To = %q, want relative", got.To)
	}
}

func TestProjectReconcilePlan_InboxFromBecomesSelector(t *testing.T) {
	in := api.ReconcilePlan{
		Token: "tok",
		Plan: api.ReconcilePlanDetail{
			Series: mustSeries(t, "Show"),
			Changes: []api.ReconcileChange{
				{
					Kind:    "add",
					Episode: mustEpisode(t, 1, 1),
					From:    "/inbox/release/x.mkv",
					To:      "/lib/Show/Season 1/x.mkv",
				},
			},
		},
	}
	out := projectReconcilePlan(in, "/lib/Show", "/inbox")
	got := out.Changes[0]
	if got.From != "inbox:release/x.mkv" {
		t.Fatalf("From = %q, want inbox selector", got.From)
	}
	if got.To != "Season 1/x.mkv" {
		t.Fatalf("To = %q, want relative", got.To)
	}
}

func TestProjectReconcilePlan_TrashItemsSurfaceAsRemovals(t *testing.T) {
	in := api.ReconcilePlan{
		Token: "tok",
		Plan: api.ReconcilePlanDetail{
			Series: mustSeries(t, "Show"),
			TrashItems: []api.ReconcileTrashChange{{
				ID:   "01H0000000000000000000AAAA",
				From: "/lib/Show/Season 1/loser.mkv",
				To:   "/lib/Show/.kura/trash/01H0000000000000000000AAAA/loser.mkv",
				Companions: []api.ReconcileMove{{
					From: "/lib/Show/Season 1/loser.en.srt",
					To:   "/lib/Show/.kura/trash/01H0000000000000000000AAAA/loser.en.srt",
				}},
			}},
		},
	}
	out := projectReconcilePlan(in, "/lib/Show", "/inbox")
	if len(out.TrashItems) != 1 {
		t.Fatalf("TrashItems len = %d, want 1", len(out.TrashItems))
	}
	tr := out.TrashItems[0]
	if tr.ID != "01H0000000000000000000AAAA" || tr.From != "Season 1/loser.mkv" {
		t.Fatalf("TrashItems[0] = %+v", tr)
	}
	if len(tr.Companions) != 1 || tr.Companions[0] != "Season 1/loser.en.srt" {
		t.Fatalf("TrashItems[0].Companions = %v, want [Season 1/loser.en.srt]", tr.Companions)
	}
	// No bucket leak anywhere in the marshalled response.
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), ".kura/trash") {
		t.Fatalf("bucket path leaked: %s", raw)
	}
}

func TestProjectReconcilePlan_ExtrasProjectNormally(t *testing.T) {
	in := api.ReconcilePlan{
		Token: "tok",
		Plan: api.ReconcilePlanDetail{
			Series: mustSeries(t, "Show"),
			Extras: []api.ReconcileExtraChange{{
				ID:     "01H0000000000000000000BBBB",
				Season: 1,
				From:   "/inbox/bts/intro.mp4",
				To:     "/lib/Show/Season 1/Extra/bts/intro.mp4",
				IsDir:  false,
			}},
		},
	}
	out := projectReconcilePlan(in, "/lib/Show", "/inbox")
	if len(out.Extras) != 1 {
		t.Fatalf("Extras len = %d", len(out.Extras))
	}
	ex := out.Extras[0]
	if ex.From != "inbox:bts/intro.mp4" {
		t.Fatalf("Extras.From = %q, want inbox selector", ex.From)
	}
	if ex.To != "Season 1/Extra/bts/intro.mp4" {
		t.Fatalf("Extras.To = %q, want relative", ex.To)
	}
}

// silence unused-helper lint when run alone.
var _ = refs.Episode{}

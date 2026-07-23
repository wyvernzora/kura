package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
)

func connectInMemoryWithStage(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	addStageTool(server, Deps{})
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

func TestKuraStage_ToolIsRegistered(t *testing.T) {
	cs := connectInMemoryWithStage(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "kura_stage" {
		t.Fatalf("unexpected tool list: %+v", res.Tools)
	}
	got := res.Tools[0]
	if got.Annotations.OpenWorldHint == nil || *got.Annotations.OpenWorldHint {
		t.Fatal("OpenWorldHint must be false (no provider)")
	}
}

func TestKuraStage_InvalidRefRejected(t *testing.T) {
	cs := connectInMemoryWithStage(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "kura_stage",
		Arguments: map[string]any{
			"ref": "not-a-ref",
			"episodes": []map[string]any{{
				"episode": "S01E01",
				"media":   "inbox:foo.mkv",
			}},
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

func TestKuraStage_RejectsOversizedBatch(t *testing.T) {
	tcases := []struct {
		field string
		args  map[string]any
	}{
		{
			field: "episodes",
			args: func() map[string]any {
				eps := make([]map[string]any, 0, maxStageBatchSize+1)
				for range maxStageBatchSize + 1 {
					eps = append(eps, map[string]any{
						"episode": "S01E01",
						"media":   "inbox:foo.mkv",
					})
				}
				return map[string]any{"ref": "tvdb:1", "episodes": eps}
			}(),
		},
		{
			field: "trash",
			args: func() map[string]any {
				items := make([]map[string]any, 0, maxStageBatchSize+1)
				for range maxStageBatchSize + 1 {
					items = append(items, map[string]any{"path": "/tmp/x"})
				}
				return map[string]any{"ref": "tvdb:1", "trash": items}
			}(),
		},
		{
			field: "extras",
			args: func() map[string]any {
				items := make([]map[string]any, 0, maxStageBatchSize+1)
				for range maxStageBatchSize + 1 {
					items = append(items, map[string]any{"season": 1, "source": "inbox:x"})
				}
				return map[string]any{"ref": "tvdb:1", "extras": items}
			}(),
		},
	}
	for _, tc := range tcases {
		t.Run(tc.field, func(t *testing.T) {
			cs := connectInMemoryWithStage(t)
			res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
				Name:      "kura_stage",
				Arguments: tc.args,
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
			if body["kind"] != errkind.KindBatchTooLarge {
				t.Fatalf("kind = %v, want %v", body["kind"], errkind.KindBatchTooLarge)
			}
		})
	}
}

func TestKuraStage_InvalidEpisodeRejected(t *testing.T) {
	cs := connectInMemoryWithStage(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "kura_stage",
		Arguments: map[string]any{
			"ref": "tvdb:1",
			"episodes": []map[string]any{{
				"episode": "garbage",
				"media":   "inbox:foo.mkv",
			}},
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

func TestStageInputToWorkflowCopiesAttrs(t *testing.T) {
	got := stageInputToWorkflow(stageInput{
		Episodes: []stageEpisodeInputItem{{
			Episode: "S01E01",
			Media:   "inbox:ep1.mkv",
			Attrs:   map[string]string{"origin": "takuhai"},
		}},
	})
	if got.Episodes[0].Attrs["origin"] != "takuhai" {
		t.Fatalf("Attrs = %#v", got.Episodes[0].Attrs)
	}
}

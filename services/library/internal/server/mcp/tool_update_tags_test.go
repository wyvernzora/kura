package mcp

import (
	"context"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library/internal/coord"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/domain/series"
	"github.com/wyvernzora/kura/services/library/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

func connectInMemoryWithUpdateTags(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	idx := indexfile.New(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	ref, err := refs.ParseSeries("Tagged Show")
	if err != nil {
		t.Fatal(err)
	}
	model := &series.Series{Ref: ref, Metadata: refs.Metadata("tvdb:42"), Episodes: map[refs.Episode]series.Episode{}}
	if err := seriesfile.SaveCAS(root, model, coord.NewMutator("seed")); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	if err := idx.SaveModel(ctx, model, coord.NewMutator("seed")); err != nil {
		t.Fatalf("SaveModel: %v", err)
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	addUpdateTagsTool(server, Deps{Workflow: workflow.Deps{
		LibRoot: root, Index: idx, Coordinator: coord.NewMCPCoordinator(), Now: time.Now,
	}})
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

func TestKuraUpdateTagsMutatesSeries(t *testing.T) {
	cs := connectInMemoryWithUpdateTags(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "kura_update_tags",
		Arguments: map[string]any{
			"ref":  "tvdb:42",
			"tags": []string{"priority", "maintenance-disabled", "!maintenance-disabled"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want conflict error")
	}

	res, err = cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "kura_update_tags",
		Arguments: map[string]any{"ref": "tvdb:42", "tags": []string{"Priority"}},
	})
	if err != nil {
		t.Fatalf("CallTool success: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true, content=%v", res.StructuredContent)
	}
	body, err := decodeStructured(res.StructuredContent)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	tags, ok := body["tags"].([]any)
	if !ok || len(tags) != 1 || tags[0] != "priority" {
		t.Fatalf("tags = %#v", body["tags"])
	}
}

func TestKuraUpdateTagsAnnotations(t *testing.T) {
	cs := connectInMemoryWithUpdateTags(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	tool := res.Tools[0]
	if tool.Name != "kura_update_tags" || tool.Annotations == nil {
		t.Fatalf("tool = %+v", tool)
	}
	if tool.Annotations.ReadOnlyHint || !tool.Annotations.IdempotentHint {
		t.Fatalf("annotations = %+v", tool.Annotations)
	}
}

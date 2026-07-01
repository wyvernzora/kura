package mcp

import (
	"context"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/textnorm"
	"github.com/wyvernzora/kura/internal/workflow"
)

func connectInMemoryWithAdd(t *testing.T) *sdkmcp.ClientSession {
	return connectInMemoryWithAddDeps(t, Deps{})
}

func connectInMemoryWithAddDeps(t *testing.T, deps Deps) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	addAddTool(server, deps)
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

type mcpMetadataSource struct{}

func (mcpMetadataSource) Key() string { return "tvdb" }

func (mcpMetadataSource) Search(context.Context, textnorm.NFCString, provider.SearchOptions) ([]provider.SearchResult, error) {
	return nil, nil
}

func (mcpMetadataSource) GetSeries(_ context.Context, metadataID, _ string) (provider.Series, error) {
	title := textnorm.NFC("Fake Show")
	return provider.Series{
		SeriesSummary: provider.SeriesSummary{
			MetadataRef:    refs.Metadata("tvdb:" + metadataID),
			PreferredTitle: title,
			CanonicalTitle: title,
		},
	}, nil
}

func mcpAddImportDeps(t *testing.T) Deps {
	t.Helper()
	libRoot := t.TempDir()
	registry := jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { registry.Shutdown(time.Second) })
	return Deps{Workflow: workflow.Deps{
		LibRoot:     libRoot,
		Index:       indexfile.New(libRoot, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()}),
		Coordinator: coord.NewCLICoordinator(),
		Provider: func() (provider.Source, error) {
			return mcpMetadataSource{}, nil
		},
		Now:  time.Now,
		Jobs: registry,
	}}
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

func TestKuraAdd_ReturnsAddResult(t *testing.T) {
	deps := mcpAddImportDeps(t)
	cs := connectInMemoryWithAddDeps(t, deps)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "kura_add",
		Arguments: map[string]any{
			"ref":     "tvdb:370070",
			"dirname": "Bookworm",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError, structured=%v", res.StructuredContent)
	}
	body, err := decodeStructured(res.StructuredContent)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["metadataRef"] != "tvdb:370070" {
		t.Fatalf("metadataRef = %v, want tvdb:370070", body["metadataRef"])
	}
	if body["ref"] != "Bookworm" {
		t.Fatalf("ref = %v, want Bookworm", body["ref"])
	}
	if body["preferredTitle"] != "Fake Show" {
		t.Fatalf("preferredTitle = %v, want Fake Show", body["preferredTitle"])
	}
}

package mcp

import (
	"context"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewServer_AdvertisesInstructions(t *testing.T) {
	ctx := context.Background()
	server := NewServer(Deps{Version: serverVersion})
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

	init := clientSession.InitializeResult()
	if init == nil {
		t.Fatal("InitializeResult is nil")
	}
	got := init.Instructions
	for _, want := range []string{
		"Kura is an anime-first library manager",
		"Never search, join, filter, or compare the Kura library by raw series names",
		"Copy strings returned by Kura tools verbatim",
		"Episode status values",
		"kura_reconcile_plan",
		"kura_job_status",
		"Permanent deletion",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Instructions missing %q:\n%s", want, got)
		}
	}
}

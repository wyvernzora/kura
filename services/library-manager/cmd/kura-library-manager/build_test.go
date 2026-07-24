package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
)

func mustParseSeries(t *testing.T, name string) refs.Series {
	t.Helper()
	ref, err := refs.ParseSeries(name)
	if err != nil {
		t.Fatalf("ParseSeries(%q): %v", name, err)
	}
	return ref
}

func TestLoadOrRebuildIndexColdRebuild(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Show"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	idx, err := loadOrRebuildIndex(context.Background(), root, indexfile.DefaultBuildOptions(), coord.NewCLICoordinator().WithIndex, nil)
	if err != nil {
		t.Fatalf("loadOrRebuildIndex: %v", err)
	}
	if err := idx.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	loaded, err := indexfile.Load(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := loaded.GetRow(mustParseSeries(t, "Show")); !ok {
		t.Fatal("rebuilt index does not include Show")
	}
}

func TestLoadOrRebuildIndexCorruptRebuild(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(paths.LibraryKuraDir(root), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(paths.IndexFile(root), []byte(`{"$schema":99999,"indexAsOf":"2026-05-04T12:00:00Z"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "Show"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	idx, err := loadOrRebuildIndex(context.Background(), root, indexfile.DefaultBuildOptions(), coord.NewCLICoordinator().WithIndex, nil)
	if err != nil {
		t.Fatalf("loadOrRebuildIndex: %v", err)
	}
	if err := idx.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	loaded, err := indexfile.Load(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := loaded.GetRow(mustParseSeries(t, "Show")); !ok {
		t.Fatal("rebuilt index does not include Show")
	}
}

package indexfile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
)

func TestIndexWatch_GetServesFromMemory(t *testing.T) {
	root := t.TempDir()
	idx := indexfile.New(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	model := minimalModel(t, "A", refs.Metadata("tvdb:1"))
	if err := idx.Upsert(indexfile.Entry{Model: model}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := idx.Get(refs.Metadata("tvdb:1"))
	if err != nil || !ok {
		t.Fatalf("Get = (%v, %v, %v); want hit", got, ok, err)
	}
	if got != model.Ref {
		t.Fatalf("Get value = %v; want A", got)
	}
}

func TestIndexWatch_RebuildCallsInjectedReader(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"A", "B"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	idx := indexfile.New(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	idx.SetEntryBuilderForTest(func(_ context.Context, _ string, ref refs.Series) (indexfile.Entry, error) {
		metadata := refs.Metadata("tvdb:100")
		if ref.String() == "B" {
			metadata = refs.Metadata("tvdb:200")
		}
		return indexfile.Entry{Model: minimalModel(t, ref.String(), metadata)}, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	idx.Watch(ctx, indexfile.WatchConfig{RebuildInterval: 30 * time.Millisecond})

	waitFor(t, func() bool {
		gotA, okA, _ := idx.Get(refs.Metadata("tvdb:100"))
		gotB, okB, _ := idx.Get(refs.Metadata("tvdb:200"))
		return okA && gotA == mustParseSeries(t, "A") && okB && gotB == mustParseSeries(t, "B")
	})
	cancel()
	if err := idx.WaitReady(t.Context()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
}

func TestIndexWatch_LibRootMTimeTriggersRebuild(t *testing.T) {
	root := t.TempDir()
	idx := indexfile.New(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	calls := make(chan refs.Series, 8)
	idx.SetEntryBuilderForTest(func(_ context.Context, _ string, ref refs.Series) (indexfile.Entry, error) {
		select {
		case calls <- ref:
		default:
		}
		return indexfile.Entry{Model: minimalModel(t, ref.String(), refs.Metadata("tvdb:300"))}, nil
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	idx.Watch(ctx, indexfile.WatchConfig{ProbeInterval: 30 * time.Millisecond})

	if err := os.Mkdir(filepath.Join(root, "BrandNew"), 0o755); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool {
		select {
		case ref := <-calls:
			return ref == mustParseSeries(t, "BrandNew")
		default:
			return false
		}
	})
	if err := idx.WaitReady(t.Context()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	cancel()
}

func TestIndexWatch_DisabledLoopsRespected(t *testing.T) {
	root := t.TempDir()
	idx := indexfile.New(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	idx.SetEntryBuilderForTest(func(context.Context, string, refs.Series) (indexfile.Entry, error) {
		t.Fatal("builder should not run when watch loops are disabled")
		return indexfile.Entry{}, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	idx.Watch(ctx, indexfile.WatchConfig{})

	if err := os.Mkdir(filepath.Join(root, "B"), 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if idx.Len() != 0 {
		t.Fatalf("Len = %d, want 0 with disabled watch loops", idx.Len())
	}
}

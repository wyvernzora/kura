package indexfile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

func writeIndexRows(t *testing.T, root string, rows []indexfile.Row) {
	t.Helper()
	if err := os.MkdirAll(paths.LibraryKuraDir(root), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := indexfile.SaveCAS(root, "", rows, coord.NewMutator("test")); err != nil {
		loaded, lerr := indexfile.LoadCAS(root)
		if lerr != nil {
			t.Fatalf("seed savecas: %v / %v", err, lerr)
		}
		if err := indexfile.SaveCAS(root, loaded.Hash, rows, coord.NewMutator("test")); err != nil {
			t.Fatalf("seed savecas2: %v", err)
		}
	}
}

func TestIndexWatch_GetServesFromMemory(t *testing.T) {
	root := t.TempDir()
	idx := indexfile.New(root)
	if err := idx.Put(refs.Metadata("tvdb:1"), mustSeries(t, "A")); err != nil {
		t.Fatal(err)
	}
	got, ok, err := idx.Get(refs.Metadata("tvdb:1"))
	if err != nil || !ok {
		t.Fatalf("Get = (%v, %v, %v); want hit", got, ok, err)
	}
	if got != mustSeries(t, "A") {
		t.Fatalf("Get value = %v; want A", got)
	}
}

func TestIndexWatch_ProbeDetectsExternalWrite(t *testing.T) {
	root := t.TempDir()
	writeIndexRows(t, root, []indexfile.Row{
		{Series: mustSeries(t, "A"), Metadata: refs.Metadata("tvdb:1")},
	})
	idx, err := indexfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	idx.Watch(ctx, indexfile.WatchConfig{ProbeInterval: 20 * time.Millisecond})

	loaded2, _ := indexfile.LoadCAS(root)
	if err := indexfile.SaveCAS(root, loaded2.Hash, []indexfile.Row{
		{Series: mustSeries(t, "A"), Metadata: refs.Metadata("tvdb:1")},
		{Series: mustSeries(t, "B"), Metadata: refs.Metadata("tvdb:2")},
	}, coord.NewMutator("peer")); err != nil {
		t.Fatalf("peer write: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(paths.LibraryKuraDir(root), "index.jsonl"), future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, ok, _ := idx.Get(refs.Metadata("tvdb:2"))
		if ok && got == mustSeries(t, "B") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("watch did not pick up external write within 2s")
}

func TestIndexWatch_RebuildCallsInjectedReader(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"A", "B"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	idx := indexfile.New(root)
	called := 0
	builder := func(_ string, ref refs.Series, _ time.Time) (indexfile.Row, error) {
		called++
		if ref == mustSeries(t, "A") {
			return indexfile.Row{Series: ref, Metadata: refs.Metadata("tvdb:100"), Title: ref.String()}, nil
		}
		return indexfile.Row{Series: ref, Metadata: refs.Metadata("tvdb:200"), Title: ref.String()}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	idx.Watch(ctx, indexfile.WatchConfig{
		RebuildInterval: 30 * time.Millisecond,
		Builder:         builder,
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, ok, _ := idx.Get(refs.Metadata("tvdb:100")); ok && got == mustSeries(t, "A") {
			if got2, ok, _ := idx.Get(refs.Metadata("tvdb:200")); ok && got2 == mustSeries(t, "B") {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("rebuild did not populate index; reader called %d times", called)
}

func TestIndexWatch_DisabledLoopsRespected(t *testing.T) {
	root := t.TempDir()
	writeIndexRows(t, root, []indexfile.Row{
		{Series: mustSeries(t, "A"), Metadata: refs.Metadata("tvdb:1")},
	})
	idx, _ := indexfile.Load(root)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	idx.Watch(ctx, indexfile.WatchConfig{})

	cur, _ := indexfile.LoadCAS(root)
	if err := indexfile.SaveCAS(root, cur.Hash, []indexfile.Row{
		{Series: mustSeries(t, "A"), Metadata: refs.Metadata("tvdb:1")},
		{Series: mustSeries(t, "B"), Metadata: refs.Metadata("tvdb:2")},
	}, coord.NewMutator("peer")); err != nil {
		t.Fatalf("peer write: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, ok, _ := idx.Get(refs.Metadata("tvdb:2")); ok {
		t.Fatal("disabled watch must not refresh; saw external row")
	}
}

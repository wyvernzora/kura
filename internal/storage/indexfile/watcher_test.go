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

func writeIndexEntries(t *testing.T, root string, entries []indexfile.Entry) {
	t.Helper()
	if err := os.MkdirAll(paths.LibraryKuraDir(root), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := indexfile.SaveCAS(root, "", entries, coord.NewMutator("test")); err != nil {
		// SaveCAS uses "" only for create. If file exists, read+rewrite.
		loaded, lerr := indexfile.LoadCAS(root)
		if lerr != nil {
			t.Fatalf("seed savecas: %v / %v", err, lerr)
		}
		if err := indexfile.SaveCAS(root, loaded.Hash, entries, coord.NewMutator("test")); err != nil {
			t.Fatalf("seed savecas2: %v", err)
		}
	}
}

func TestWatcher_GetServesFromMemory(t *testing.T) {
	root := t.TempDir()
	idx := indexfile.New(root)
	if err := idx.Put(refs.Metadata("tvdb:1"), mustSeries(t, "A")); err != nil {
		t.Fatal(err)
	}
	w := indexfile.NewWatcher(idx, nil, indexfile.WatcherConfig{}, nil)
	got, ok, err := w.Index().Get(refs.Metadata("tvdb:1"))
	if err != nil || !ok {
		t.Fatalf("Get = (%v, %v, %v); want hit", got, ok, err)
	}
	if got != mustSeries(t, "A") {
		t.Fatalf("Get value = %v; want A", got)
	}
}

func TestWatcher_ProbeDetectsExternalWrite(t *testing.T) {
	root := t.TempDir()
	writeIndexEntries(t, root, []indexfile.Entry{
		{Metadata: refs.Metadata("tvdb:1"), Series: mustSeries(t, "A")},
	})
	loaded, err := indexfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	w := indexfile.NewWatcher(loaded, nil, indexfile.WatcherConfig{
		ProbeInterval: 20 * time.Millisecond,
	}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w.Run(ctx)

	// External peer rewrites the index with a second entry. Bump
	// mtime explicitly so the probe sees a change even on filesystems
	// with coarse mtime granularity.
	loaded2, _ := indexfile.LoadCAS(root)
	if err := indexfile.SaveCAS(root, loaded2.Hash, []indexfile.Entry{
		{Metadata: refs.Metadata("tvdb:1"), Series: mustSeries(t, "A")},
		{Metadata: refs.Metadata("tvdb:2"), Series: mustSeries(t, "B")},
	}, coord.NewMutator("peer")); err != nil {
		t.Fatalf("peer write: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(paths.LibraryKuraDir(root), "index.tsv"), future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, ok, _ := w.Index().Get(refs.Metadata("tvdb:2"))
		if ok && got == mustSeries(t, "B") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("watcher did not pick up external write within 2s")
}

func TestWatcher_RebuildCallsInjectedReader(t *testing.T) {
	root := t.TempDir()
	// Seed two series directories so Rebuild visits them.
	for _, name := range []string{"A", "B"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	idx := indexfile.New(root)
	called := 0
	reader := func(_ context.Context, ref refs.Series) (refs.Metadata, error) {
		called++
		if ref == mustSeries(t, "A") {
			return refs.Metadata("tvdb:100"), nil
		}
		return refs.Metadata("tvdb:200"), nil
	}
	w := indexfile.NewWatcher(idx, reader, indexfile.WatcherConfig{
		RebuildInterval: 30 * time.Millisecond,
	}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, ok, _ := w.Index().Get(refs.Metadata("tvdb:100")); ok && got == mustSeries(t, "A") {
			if got2, ok, _ := w.Index().Get(refs.Metadata("tvdb:200")); ok && got2 == mustSeries(t, "B") {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("rebuild did not populate index; reader called %d times", called)
}

func TestWatcher_DisabledLoopsRespected(t *testing.T) {
	root := t.TempDir()
	writeIndexEntries(t, root, []indexfile.Entry{
		{Metadata: refs.Metadata("tvdb:1"), Series: mustSeries(t, "A")},
	})
	loaded, _ := indexfile.Load(root)
	w := indexfile.NewWatcher(loaded, nil, indexfile.WatcherConfig{
		ProbeInterval:   0,
		RefreshInterval: 0,
		RebuildInterval: 0,
	}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w.Run(ctx)

	// External peer adds a second entry. With all loops disabled the
	// in-memory Index must remain stale.
	cur, _ := indexfile.LoadCAS(root)
	if err := indexfile.SaveCAS(root, cur.Hash, []indexfile.Entry{
		{Metadata: refs.Metadata("tvdb:1"), Series: mustSeries(t, "A")},
		{Metadata: refs.Metadata("tvdb:2"), Series: mustSeries(t, "B")},
	}, coord.NewMutator("peer")); err != nil {
		t.Fatalf("peer write: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, ok, _ := w.Index().Get(refs.Metadata("tvdb:2")); ok {
		t.Fatal("disabled watcher must not refresh; saw external entry")
	}
}

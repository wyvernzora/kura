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

func TestIndexWatch_LibRootMTimeTriggersRebuild(t *testing.T) {
	root := t.TempDir()
	// Seed an empty index.jsonl so Load doesn't fail.
	writeIndexRows(t, root, []indexfile.Row{
		{Series: mustSeries(t, "Existing"), Metadata: refs.Metadata("tvdb:1"), Title: "Existing"},
	})
	idx, err := indexfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	calls := make(chan refs.Series, 8)
	builder := func(_ string, ref refs.Series, now time.Time) (indexfile.Row, error) {
		select {
		case calls <- ref:
		default:
		}
		return indexfile.Row{Series: ref, Metadata: refs.Metadata("tvdb:" + ref.String()), Title: ref.String()}, nil
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	idx.Watch(ctx, indexfile.WatchConfig{
		ProbeInterval: 30 * time.Millisecond,
		Builder:       builder,
	})

	// mkdir under libRoot — should bump libRoot mtime within the
	// next probe tick, triggering a rebuild that walks the new dir.
	// Force an artificially-old baseline first so the Stat() bump is
	// guaranteed to differ.
	past := time.Now().Add(-time.Hour)
	_ = os.Chtimes(root, past, past)
	if err := os.Mkdir(filepath.Join(root, "BrandNew"), 0o755); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ref := <-calls:
			if ref == mustSeries(t, "BrandNew") {
				if err := idx.WaitReady(t.Context()); err != nil {
					t.Fatalf("WaitReady: %v", err)
				}
				return
			}
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatal("libRoot probe never triggered a rebuild that walked BrandNew within 2s")
}

// writeSchemaMismatchedIndex writes a JSONL file whose header carries
// a future SchemaVersion. ParseCAS returns ErrSchemaMismatch on read.
// Used to drive the watcher's schema-mismatch recovery path.
func writeSchemaMismatchedIndex(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(paths.LibraryKuraDir(root), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Header schema bumped past anything the build understands.
	body := []byte(`{"$schema":99999,"indexAsOf":"2026-05-04T12:00:00Z"}` + "\n")
	if err := os.WriteFile(paths.IndexFile(root), body, 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}
}

func TestIndexWatch_SchemaMismatchTriggersRebuild(t *testing.T) {
	root := t.TempDir()
	// Plant a future-schema file and one tracked dir for the rebuild
	// to pick up. Index starts empty (Load would fail with mismatch).
	writeSchemaMismatchedIndex(t, root)
	if err := os.Mkdir(filepath.Join(root, "Replacement"), 0o755); err != nil {
		t.Fatal(err)
	}

	idx := indexfile.New(root)
	calls := make(chan refs.Series, 8)
	builder := func(_ string, ref refs.Series, _ time.Time) (indexfile.Row, error) {
		select {
		case calls <- ref:
		default:
		}
		return indexfile.Row{Series: ref, Metadata: refs.Metadata("tvdb:" + ref.String()), Title: ref.String()}, nil
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	idx.Watch(ctx, indexfile.WatchConfig{
		ProbeInterval: 30 * time.Millisecond,
		Builder:       builder,
	})

	// Probe sees the on-disk file (mtime/size differs from the
	// uninitialised baseline), calls fullRefresh → ParseCAS returns
	// ErrSchemaMismatch → handleRefreshError fires TriggerRebuild.
	// The rebuild walks libRoot and indexes Replacement.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ref := <-calls:
			if ref == mustSeries(t, "Replacement") {
				// Drain rebuild before TempDir cleanup races the
				// goroutine still writing index.jsonl.
				if err := idx.WaitReady(t.Context()); err != nil {
					t.Fatalf("WaitReady: %v", err)
				}
				return
			}
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatal("schema-mismatch handler never triggered a rebuild that walked Replacement within 2s")
}

func TestIndexWatch_SchemaMismatchOverwritesStaleFile(t *testing.T) {
	root := t.TempDir()
	writeSchemaMismatchedIndex(t, root)
	if err := os.Mkdir(filepath.Join(root, "Fresh"), 0o755); err != nil {
		t.Fatal(err)
	}

	idx := indexfile.New(root)
	builder := func(_ string, ref refs.Series, _ time.Time) (indexfile.Row, error) {
		return indexfile.Row{Series: ref, Metadata: refs.Metadata("tvdb:" + ref.String()), Title: ref.String()}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	idx.Watch(ctx, indexfile.WatchConfig{
		ProbeInterval: 30 * time.Millisecond,
		Builder:       builder,
	})

	// Wait for rebuild to complete by checking that the new index has
	// the current SchemaVersion (vs the planted 99999).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		loaded, err := indexfile.LoadCAS(root)
		if err == nil && loaded.Header.SchemaVersion == indexfile.SchemaVersion {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("schema-mismatched file was not overwritten with current SchemaVersion within 2s")
}

func TestIndexWatch_BuildOptionsMismatchOverwritesStaleFile(t *testing.T) {
	root := t.TempDir()
	oldOpts := indexfile.DefaultBuildOptions()
	oldOpts.AiringTailDays = 3
	if err := indexfile.SaveCASWithOptions(root, "", []indexfile.Row{
		{Series: mustSeries(t, "Stale"), Metadata: refs.Metadata("tvdb:stale"), Title: "Stale"},
	}, coord.NewMutator("seed"), oldOpts); err != nil {
		t.Fatalf("SaveCASWithOptions: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "Fresh"), 0o755); err != nil {
		t.Fatal(err)
	}

	idx, err := indexfile.LoadWithOptions(root, oldOpts)
	if err != nil {
		t.Fatalf("LoadWithOptions: %v", err)
	}
	opts := indexfile.DefaultBuildOptions()
	builder := indexfile.NewRowBuilder(opts)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	idx.Watch(ctx, indexfile.WatchConfig{
		ProbeInterval: 30 * time.Millisecond,
		Builder:       builder,
		BuildOptions:  &opts,
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		loaded, err := indexfile.LoadCAS(root)
		if err == nil && loaded.Header.BuildOptions != nil && *loaded.Header.BuildOptions == opts {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("build-options-mismatched file was not overwritten with default BuildOptions within 2s")
}

// SaveAndAdopt must bump the watcher's probe baseline (hash + mtime +
// size) to the just-written file's values. Without this, the next
// probe tick observes drift against our own write and fires a no-op
// fullRefresh.
func TestIndexWatch_SaveAndAdoptBumpsProbeCache(t *testing.T) {
	root := t.TempDir()
	writeIndexRows(t, root, []indexfile.Row{
		{Series: mustSeries(t, "A"), Metadata: refs.Metadata("tvdb:1")},
	})
	idx, err := indexfile.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// Long probe interval so the loop never naturally fires; we assert
	// against the cached baseline directly.
	idx.Watch(ctx, indexfile.WatchConfig{ProbeInterval: time.Hour})

	cur, _ := indexfile.LoadCAS(root)
	rows := []indexfile.Row{
		{Series: mustSeries(t, "A"), Metadata: refs.Metadata("tvdb:1")},
		{Series: mustSeries(t, "B"), Metadata: refs.Metadata("tvdb:2")},
	}
	if err := idx.SaveAndAdopt(cur.Hash, rows, coord.NewMutator("test")); err != nil {
		t.Fatalf("SaveAndAdopt: %v", err)
	}

	stat, err := os.Stat(filepath.Join(paths.LibraryKuraDir(root), "index.jsonl"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	post, _ := indexfile.LoadCAS(root)

	cachedHash, cachedMTime, cachedSize := idx.ProbeBaselineForTest()
	if cachedHash != post.Hash {
		t.Fatalf("cachedHash = %s, want %s (post-write hash)", cachedHash, post.Hash)
	}
	if !cachedMTime.Equal(stat.ModTime()) {
		t.Fatalf("cachedMTime = %v, want %v (post-write mtime)", cachedMTime, stat.ModTime())
	}
	if cachedSize != stat.Size() {
		t.Fatalf("cachedSize = %d, want %d (post-write size)", cachedSize, stat.Size())
	}
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

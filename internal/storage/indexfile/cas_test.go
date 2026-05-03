package indexfile_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

func mustParseSeries(t *testing.T, name string) refs.Series {
	t.Helper()
	ref, err := refs.ParseSeries(name)
	if err != nil {
		t.Fatalf("ParseSeries(%q): %v", name, err)
	}
	return ref
}

func TestSaveCASCreateThenLoad(t *testing.T) {
	root := t.TempDir()
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "ws", At: time.Date(2026, 5, 2, 19, 14, 0, 0, time.UTC)}
	entries := []indexfile.Entry{
		{Metadata: refs.Metadata("tvdb:42"), Series: mustParseSeries(t, "Show A")},
		{Metadata: refs.Metadata("tvdb:7"), Series: mustParseSeries(t, "Show B")},
	}
	if err := indexfile.SaveCAS(root, "", entries, mutator); err != nil {
		t.Fatalf("SaveCAS create: %v", err)
	}

	loaded, err := indexfile.LoadCAS(root)
	if err != nil {
		t.Fatalf("LoadCAS: %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(loaded.Entries))
	}
	if loaded.Hash == "" {
		t.Fatal("Hash empty")
	}
	if loaded.LastMutated == nil {
		t.Fatal("LastMutated not parsed from header")
	}
	if loaded.LastMutated.Op != "add" || loaded.LastMutated.PID != 1 {
		t.Fatalf("LastMutated = %+v", loaded.LastMutated)
	}

	// Entries should be sorted by metadata ref.
	if loaded.Entries[0].Metadata.String() != "tvdb:42" {
		// Lexical sort: "tvdb:42" < "tvdb:7" because "4" < "7".
		t.Fatalf("Entries[0] = %s, want tvdb:42", loaded.Entries[0].Metadata)
	}
}

func TestSaveCASUpdatePreservesHashRoundtrip(t *testing.T) {
	root := t.TempDir()
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "ws", At: time.Now().UTC()}
	if err := indexfile.SaveCAS(root, "", []indexfile.Entry{
		{Metadata: refs.Metadata("tvdb:1"), Series: mustParseSeries(t, "A")},
	}, mutator); err != nil {
		t.Fatalf("create: %v", err)
	}
	first, err := indexfile.LoadCAS(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Append entry via CAS.
	updated := append(first.Entries, indexfile.Entry{Metadata: refs.Metadata("tvdb:2"), Series: mustParseSeries(t, "B")})
	if err := indexfile.SaveCAS(root, first.Hash, updated, mutator); err != nil {
		t.Fatalf("update: %v", err)
	}

	second, err := indexfile.LoadCAS(root)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if second.Hash == first.Hash {
		t.Fatal("hash unchanged after update")
	}
	if len(second.Entries) != 2 {
		t.Fatalf("entries = %d", len(second.Entries))
	}
}

func TestSaveCASDetectsPreWriteDrift(t *testing.T) {
	root := t.TempDir()
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "ws", At: time.Now().UTC()}
	if err := indexfile.SaveCAS(root, "", []indexfile.Entry{
		{Metadata: refs.Metadata("tvdb:1"), Series: mustParseSeries(t, "A")},
	}, mutator); err != nil {
		t.Fatalf("create: %v", err)
	}
	first, _ := indexfile.LoadCAS(root)

	// Peer mutation lands.
	peer := append(first.Entries, indexfile.Entry{Metadata: refs.Metadata("tvdb:peer"), Series: mustParseSeries(t, "Peer")})
	peerMutator := coord.Mutator{Op: "add", PID: 999, Host: "ws", At: time.Now().UTC()}
	if err := indexfile.SaveCAS(root, first.Hash, peer, peerMutator); err != nil {
		t.Fatalf("peer SaveCAS: %v", err)
	}

	// Our save with stale hash should conflict, surfacing peer mutator.
	err := indexfile.SaveCAS(root, first.Hash, first.Entries, mutator)
	var conflict *coord.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want ConflictError", err)
	}
	if conflict.Mutator.PID != 999 {
		t.Fatalf("conflict mutator PID = %d, want 999", conflict.Mutator.PID)
	}
}

func TestSaveCASCreateConflictsIfFileExists(t *testing.T) {
	root := t.TempDir()
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "ws", At: time.Now().UTC()}
	if err := indexfile.SaveCAS(root, "", []indexfile.Entry{}, mutator); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := indexfile.SaveCAS(root, "", []indexfile.Entry{}, mutator)
	var conflict *coord.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want ConflictError", err)
	}
}

func TestSaveCASUpdateOnMissingFileReportsConflict(t *testing.T) {
	root := t.TempDir()
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "ws", At: time.Now().UTC()}
	err := indexfile.SaveCAS(root, "deadbeef", []indexfile.Entry{}, mutator)
	var conflict *coord.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want ConflictError when file missing", err)
	}
}

func TestParseCASLegacyFileWithoutHeader(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(paths.LibraryKuraDir(root), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := []byte("tvdb:1\tShow A\ntvdb:2\tShow B\n")
	if err := os.WriteFile(paths.IndexFile(root), legacy, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := indexfile.LoadCAS(root)
	if err != nil {
		t.Fatalf("LoadCAS: %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(loaded.Entries))
	}
	if loaded.LastMutated != nil {
		t.Fatalf("LastMutated = %+v, want nil for legacy file", loaded.LastMutated)
	}
}

func TestReadHashOnly(t *testing.T) {
	root := t.TempDir()
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "ws", At: time.Now().UTC()}
	if err := indexfile.SaveCAS(root, "", []indexfile.Entry{
		{Metadata: refs.Metadata("tvdb:1"), Series: mustParseSeries(t, "A")},
	}, mutator); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	hash, err := indexfile.ReadHashOnly(root)
	if err != nil {
		t.Fatalf("ReadHashOnly: %v", err)
	}
	loaded, _ := indexfile.LoadCAS(root)
	if hash != loaded.Hash {
		t.Fatalf("hash mismatch: %s vs %s", hash, loaded.Hash)
	}
}

func TestSaveCASMutatorHostWithSpaces(t *testing.T) {
	root := t.TempDir()
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "my host", At: time.Now().UTC()}
	if err := indexfile.SaveCAS(root, "", []indexfile.Entry{}, mutator); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	loaded, err := indexfile.LoadCAS(root)
	if err != nil {
		t.Fatalf("LoadCAS: %v", err)
	}
	if loaded.LastMutated == nil || loaded.LastMutated.Host != "my host" {
		t.Fatalf("Host = %v", loaded.LastMutated)
	}
}

func TestParseCASRejectsMalformedRows(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(paths.LibraryKuraDir(root), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	bad := []byte("only-one-field\n")
	if err := os.WriteFile(filepath.Join(paths.LibraryKuraDir(root), "index.tsv"), bad, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := indexfile.LoadCAS(root)
	if err == nil || !strings.Contains(err.Error(), "indexfile: parse") {
		t.Fatalf("err = %v, want parse error", err)
	}
}

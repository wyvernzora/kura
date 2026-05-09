package indexfile_test

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/response"
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
	rows := []indexfile.Row{
		{Series: mustParseSeries(t, "Show A"), Metadata: refs.Metadata("tvdb:42"), Title: "Show A", Status: response.ListStatusComplete},
		{Series: mustParseSeries(t, "Show B"), Metadata: refs.Metadata("tvdb:7"), Title: "Show B", Status: response.ListStatusComplete},
	}
	if err := indexfile.SaveCAS(root, "", rows, mutator); err != nil {
		t.Fatalf("SaveCAS create: %v", err)
	}

	loaded, err := indexfile.LoadCAS(root)
	if err != nil {
		t.Fatalf("LoadCAS: %v", err)
	}
	if len(loaded.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(loaded.Rows))
	}
	if loaded.Hash == "" {
		t.Fatal("Hash empty")
	}
	if loaded.Header.SchemaVersion != indexfile.SchemaVersion {
		t.Fatalf("Header.SchemaVersion = %d, want %d", loaded.Header.SchemaVersion, indexfile.SchemaVersion)
	}
	if loaded.Header.LastMutated == nil {
		t.Fatal("LastMutated not parsed from header")
	}
	if loaded.Header.LastMutated.Op != "add" || loaded.Header.LastMutated.PID != 1 {
		t.Fatalf("LastMutated = %+v", loaded.Header.LastMutated)
	}

	// Rows are sorted by series ref.
	if loaded.Rows[0].Series.String() != "Show A" {
		t.Fatalf("Rows[0] = %s, want Show A", loaded.Rows[0].Series)
	}
}

func TestSaveCASRoundTripIsByteStable(t *testing.T) {
	root := t.TempDir()
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "ws", At: time.Date(2026, 5, 2, 19, 14, 0, 0, time.UTC)}
	rows := []indexfile.Row{
		{Series: mustParseSeries(t, "Show A"), Metadata: refs.Metadata("tvdb:42"), Title: "Show A", Status: response.ListStatusComplete},
		{Series: mustParseSeries(t, "Show B"), Metadata: refs.Metadata("tvdb:7"), Title: "Show B", Status: response.ListStatusIncomplete},
	}
	if err := indexfile.SaveCAS(root, "", rows, mutator); err != nil {
		t.Fatalf("first save: %v", err)
	}
	first, err := os.ReadFile(paths.IndexFile(root))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := os.Remove(paths.IndexFile(root)); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// Reorder rows; output should still be byte-identical because encode
	// sorts by series ref before writing.
	reordered := []indexfile.Row{rows[1], rows[0]}
	if err := indexfile.SaveCAS(root, "", reordered, mutator); err != nil {
		t.Fatalf("second save: %v", err)
	}
	second, err := os.ReadFile(paths.IndexFile(root))
	if err != nil {
		t.Fatalf("read 2: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("byte-stable round-trip violated:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestSaveCASUpdatePreservesHashRoundtrip(t *testing.T) {
	root := t.TempDir()
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "ws", At: time.Now().UTC()}
	if err := indexfile.SaveCAS(root, "", []indexfile.Row{
		{Series: mustParseSeries(t, "A"), Metadata: refs.Metadata("tvdb:1"), Title: "A", Status: response.ListStatusComplete},
	}, mutator); err != nil {
		t.Fatalf("create: %v", err)
	}
	first, err := indexfile.LoadCAS(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Append row via CAS. Clone first.Rows so the append doesn't
	// alias into the LoadCAS-returned slice's backing array.
	updated := append(slices.Clone(first.Rows), indexfile.Row{Series: mustParseSeries(t, "B"), Metadata: refs.Metadata("tvdb:2"), Title: "B", Status: response.ListStatusComplete})
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
	if len(second.Rows) != 2 {
		t.Fatalf("rows = %d", len(second.Rows))
	}
}

func TestSaveCASDetectsPreWriteDrift(t *testing.T) {
	root := t.TempDir()
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "ws", At: time.Now().UTC()}
	if err := indexfile.SaveCAS(root, "", []indexfile.Row{
		{Series: mustParseSeries(t, "A"), Metadata: refs.Metadata("tvdb:1"), Title: "A", Status: response.ListStatusComplete},
	}, mutator); err != nil {
		t.Fatalf("create: %v", err)
	}
	first, _ := indexfile.LoadCAS(root)

	// Peer mutation lands. Clone first.Rows so the append doesn't
	// alias into the LoadCAS-returned slice's backing array.
	peer := append(slices.Clone(first.Rows), indexfile.Row{Series: mustParseSeries(t, "Peer"), Metadata: refs.Metadata("tvdb:peer"), Title: "Peer", Status: response.ListStatusComplete})
	peerMutator := coord.Mutator{Op: "add", PID: 999, Host: "ws", At: time.Now().UTC()}
	if err := indexfile.SaveCAS(root, first.Hash, peer, peerMutator); err != nil {
		t.Fatalf("peer SaveCAS: %v", err)
	}

	// Our save with stale hash should conflict, surfacing peer mutator.
	err := indexfile.SaveCAS(root, first.Hash, first.Rows, mutator)
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
	if err := indexfile.SaveCAS(root, "", []indexfile.Row{}, mutator); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := indexfile.SaveCAS(root, "", []indexfile.Row{}, mutator)
	var conflict *coord.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want ConflictError", err)
	}
}

func TestSaveCASUpdateOnMissingFileReportsConflict(t *testing.T) {
	root := t.TempDir()
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "ws", At: time.Now().UTC()}
	err := indexfile.SaveCAS(root, "deadbeef", []indexfile.Row{}, mutator)
	var conflict *coord.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want ConflictError when file missing", err)
	}
}

func TestParseCASRejectsLegacyTSV(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(paths.LibraryKuraDir(root), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Old TSV format (no JSON header) must not parse as v2 JSONL.
	legacy := []byte("tvdb:1\tShow A\ntvdb:2\tShow B\n")
	if err := os.WriteFile(paths.IndexFile(root), legacy, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := indexfile.LoadCAS(root)
	if err == nil {
		t.Fatal("LoadCAS on TSV bytes should fail")
	}
	if !strings.Contains(err.Error(), "indexfile: parse") {
		t.Fatalf("err = %v, want parse error", err)
	}
}

func TestParseCASRejectsSchemaMismatch(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(paths.LibraryKuraDir(root), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	bogus := []byte(`{"$schema":99,"indexAsOf":"2026-01-01T00:00:00Z"}` + "\n")
	if err := os.WriteFile(paths.IndexFile(root), bogus, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := indexfile.LoadCAS(root)
	if !errors.Is(err, indexfile.ErrSchemaMismatch) {
		t.Fatalf("err = %v, want ErrSchemaMismatch", err)
	}
}

func TestParseCASRejectsMalformedRows(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(paths.LibraryKuraDir(root), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	bad := []byte(`{"$schema":3,"indexAsOf":"2026-01-01T00:00:00Z"}` + "\n" + `not-json` + "\n")
	if err := os.WriteFile(paths.IndexFile(root), bad, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := indexfile.LoadCAS(root)
	if err == nil || !strings.Contains(err.Error(), "indexfile: parse row") {
		t.Fatalf("err = %v, want parse-row error", err)
	}
}

func TestReadHashOnly(t *testing.T) {
	root := t.TempDir()
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "ws", At: time.Now().UTC()}
	if err := indexfile.SaveCAS(root, "", []indexfile.Row{
		{Series: mustParseSeries(t, "A"), Metadata: refs.Metadata("tvdb:1"), Title: "A", Status: response.ListStatusComplete},
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
	if err := indexfile.SaveCAS(root, "", []indexfile.Row{}, mutator); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	loaded, err := indexfile.LoadCAS(root)
	if err != nil {
		t.Fatalf("LoadCAS: %v", err)
	}
	if loaded.Header.LastMutated == nil || loaded.Header.LastMutated.Host != "my host" {
		t.Fatalf("Host = %v", loaded.Header.LastMutated)
	}
}

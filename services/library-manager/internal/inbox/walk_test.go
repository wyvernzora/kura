package inbox_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/inbox"
	"golang.org/x/text/unicode/norm"
)

// makeTree creates files+dirs under root from a slash-keyed map. Each
// value is "f" (empty file), "d" (dir), or starts with "f:" followed
// by content bytes.
func makeTree(t *testing.T, root string, layout map[string]string) {
	t.Helper()
	keys := make([]string, 0, len(layout))
	for k := range layout {
		keys = append(keys, k)
	}
	sort.Strings(keys) // dirs first by virtue of being shorter
	for _, p := range keys {
		full := filepath.Join(root, filepath.FromSlash(p))
		switch v := layout[p]; {
		case v == "d":
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", full, err)
			}
		case v == "f" || strings.HasPrefix(v, "f:"):
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("mkdir parent %s: %v", filepath.Dir(full), err)
			}
			body := []byte{}
			if strings.HasPrefix(v, "f:") {
				body = []byte(v[2:])
			}
			if err := os.WriteFile(full, body, 0o644); err != nil {
				t.Fatalf("write %s: %v", full, err)
			}
		}
	}
}

func defaultOpts() inbox.Options {
	return inbox.Options{Limit: 500}
}

func TestWalk_NonRecursiveListsImmediate(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, map[string]string{
		"a.mkv":     "f",
		"b.mkv":     "f",
		"sub":       "d",
		"sub/c.mkv": "f",
	})
	res, err := inbox.Walk(root, defaultOpts())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := relPaths(res.Entries)
	want := []string{"a.mkv", "b.mkv", "sub"}
	if !equalSet(got, want) {
		t.Errorf("immediate listing: got %v, want %v", got, want)
	}
}

func TestWalk_RecursiveBoundedByDepth(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, map[string]string{
		"l1":          "d",
		"l1/a.mkv":    "f",
		"l1/l2":       "d",
		"l1/l2/b.mkv": "f",
		"l1/l2/l3":    "d",
		"l1/l2/l3/c":  "f",
	})
	opts := defaultOpts()
	opts.Recursive = true
	opts.Depth = 2 // l1 + l1/* but not l1/l2/*

	res, err := inbox.Walk(root, opts)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := relPaths(res.Entries)
	want := []string{"l1", "l1/a.mkv", "l1/l2"}
	if !equalSet(got, want) {
		t.Errorf("depth=2: got %v, want %v", got, want)
	}
}

func TestWalk_HiddenFilteredByDefault(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, map[string]string{
		"a.mkv":          "f",
		".hidden":        "f",
		"download.tmp":   "f",
		"foo.crdownload": "f",
		"qbpartial.!qB":  "f",
	})
	res, err := inbox.Walk(root, defaultOpts())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := relPaths(res.Entries)
	if !equalSet(got, []string{"a.mkv"}) {
		t.Errorf("hidden default skip: got %v, want only [a.mkv]", got)
	}

	opts := defaultOpts()
	opts.IncludeHidden = true
	res2, err := inbox.Walk(root, opts)
	if err != nil {
		t.Fatalf("Walk all: %v", err)
	}
	got2 := relPaths(res2.Entries)
	if len(got2) != 5 {
		t.Errorf("IncludeHidden: got %d entries, want 5: %v", len(got2), got2)
	}
}

func TestWalk_NameGlob(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, map[string]string{
		"a.mkv": "f",
		"b.srt": "f",
		"c.mkv": "f",
	})
	opts := defaultOpts()
	opts.NameGlob = "*.mkv"
	res, err := inbox.Walk(root, opts)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := relPaths(res.Entries)
	if !equalSet(got, []string{"a.mkv", "c.mkv"}) {
		t.Errorf("glob: got %v", got)
	}
}

func TestWalk_KindFilter(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, map[string]string{
		"a.mkv": "f",
		"sub":   "d",
	})
	opts := defaultOpts()
	opts.Kind = "file"
	res, err := inbox.Walk(root, opts)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !equalSet(relPaths(res.Entries), []string{"a.mkv"}) {
		t.Errorf("kind=file: got %v", relPaths(res.Entries))
	}
}

func TestWalk_TruncationAndElided(t *testing.T) {
	root := t.TempDir()
	tree := map[string]string{}
	for i := range 10 {
		tree[mkName(i)] = "f"
	}
	makeTree(t, root, tree)
	opts := defaultOpts()
	opts.Limit = 3
	res, err := inbox.Walk(root, opts)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !res.Truncated {
		t.Error("expected Truncated=true")
	}
	if res.ElidedCount != 7 {
		t.Errorf("ElidedCount: got %d, want 7", res.ElidedCount)
	}
	if len(res.Entries) != 3 {
		t.Errorf("Entries: got %d, want 3", len(res.Entries))
	}
}

func TestWalk_SortMtimeDescNameAscTiebreak(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, map[string]string{
		"old1.mkv": "f",
		"old2.mkv": "f",
		"new.mkv":  "f",
	})
	// Backdate old*.mkv 1 hour.
	past := time.Now().Add(-time.Hour)
	for _, n := range []string{"old1.mkv", "old2.mkv"} {
		if err := os.Chtimes(filepath.Join(root, n), past, past); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	res, err := inbox.Walk(root, defaultOpts())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := relPaths(res.Entries)
	want := []string{"new.mkv", "old1.mkv", "old2.mkv"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sort: got %v, want %v", got, want)
			break
		}
	}
}

func TestWalk_TraversalRejectedDotDot(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, map[string]string{"a.mkv": "f"})
	opts := defaultOpts()
	opts.Path = "../etc"
	if _, err := inbox.Walk(root, opts); err == nil {
		t.Fatal("expected error for ../etc")
	}
}

func TestWalk_PathNotFound(t *testing.T) {
	root := t.TempDir()
	opts := defaultOpts()
	opts.Path = "nope"
	_, err := inbox.Walk(root, opts)
	var nfErr *inbox.PathNotFoundError
	if !errors.As(err, &nfErr) {
		t.Fatalf("expected PathNotFoundError, got %T %v", err, err)
	}
}

func TestWalk_FilePathReturnsFile(t *testing.T) {
	root := t.TempDir()
	const name = "[LoliHouse] Tenbin - 02 [WebRip 1080p HEVC-10bit AAC ASSx2].mkv"
	makeTree(t, root, map[string]string{name: "f:data"})
	opts := defaultOpts()
	opts.Path = name
	opts.Depth = 3

	res, err := inbox.Walk(root, opts)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if res.Path != name {
		t.Errorf("Path: got %q, want %q", res.Path, name)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("Entries: got %d, want 1", len(res.Entries))
	}
	entry := res.Entries[0]
	if entry.Name != name {
		t.Errorf("Name: got %q, want %q", entry.Name, name)
	}
	if entry.RelPath != name {
		t.Errorf("RelPath: got %q, want %q", entry.RelPath, name)
	}
	if entry.Kind != inbox.KindFile {
		t.Errorf("Kind: got %q, want %q", entry.Kind, inbox.KindFile)
	}
	if entry.Size != 4 {
		t.Errorf("Size: got %d, want 4", entry.Size)
	}
}

func TestWalk_SymlinkSurfaceNoFollow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks awkward on windows")
	}
	root := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "outside.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	makeTree(t, root, map[string]string{"a.mkv": "f"})

	res, err := inbox.Walk(root, defaultOpts())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	var sawLink bool
	for _, e := range res.Entries {
		if e.Name == "linked" {
			sawLink = true
			if e.Kind != inbox.KindSymlink {
				t.Errorf("linked kind: got %s, want symlink", e.Kind)
			}
			if e.SymlinkTarget == "" {
				t.Errorf("linked SymlinkTarget should be populated")
			}
		}
		if strings.Contains(e.RelPath, "outside.mkv") {
			t.Errorf("walker followed symlink: saw %s", e.RelPath)
		}
	}
	if !sawLink {
		t.Errorf("symlink entry missing")
	}

	opts := defaultOpts()
	opts.Path = "linked"
	if _, err := inbox.Walk(root, opts); err == nil {
		t.Fatal("expected exact symlink path to be rejected")
	}
}

func TestWalk_NFCFromOSReadDir(t *testing.T) {
	// Construct a filename in NFD on disk; walker must surface it as
	// NFC. macOS HFS+/APFS will store this as NFD natively; on Linux
	// we explicitly write the NFD bytes.
	root := t.TempDir()
	nfdName := norm.NFD.String("café.mkv")
	if err := os.WriteFile(filepath.Join(root, nfdName), []byte{}, 0o644); err != nil {
		t.Skipf("filesystem rejected NFD name: %v", err)
	}
	res, err := inbox.Walk(root, defaultOpts())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(res.Entries) == 0 {
		t.Fatal("no entries")
	}
	got := res.Entries[0].Name
	want := norm.NFC.String("café.mkv")
	if got != want {
		t.Errorf("name normalization: got %q, want %q (NFC)", got, want)
	}
	if !norm.NFC.IsNormalString(got) {
		t.Errorf("entry name is not NFC: %q", got)
	}
	// Confirm RelPath is also NFC.
	if !norm.NFC.IsNormalString(res.Entries[0].RelPath) {
		t.Errorf("RelPath is not NFC: %q", res.Entries[0].RelPath)
	}
}

func TestEntry_Selector(t *testing.T) {
	e := inbox.Entry{Name: "foo.mkv", RelPath: "[BDrip] x/foo.mkv"}
	sel := e.Selector()
	if sel.String() != "inbox:[BDrip] x/foo.mkv" {
		t.Errorf("Selector: got %q", sel.String())
	}
}

// Helpers

func relPaths(entries []inbox.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.RelPath
	}
	return out
}

func equalSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	am := map[string]int{}
	for _, s := range a {
		am[s]++
	}
	for _, s := range b {
		am[s]--
		if am[s] < 0 {
			return false
		}
	}
	return true
}

func mkName(i int) string {
	return strings.Repeat("a", 1) + "_" + string(rune('a'+i)) + ".mkv"
}

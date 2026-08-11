package fingerprint_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/fingerprint"
)

func TestOfCanonicalSerialization(t *testing.T) {
	entries := []fingerprint.Entry{
		{Path: "z.mkv", Size: 12, ModTime: time.Unix(20, 900_000_000)},
		{Path: "a.ass", Size: 4, ModTime: time.Unix(10, 100_000_000)},
	}

	got, err := fingerprint.Of(entries)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	want := fingerprint.Fingerprint(
		"sha256:0cf894b04cc6fd2d4e2fa97b266953f2315cb8fecbf05c452b1e1c428c5c8a2b",
	)
	if got != want {
		t.Fatalf("Of = %q, want %q", got, want)
	}

	reordered, err := fingerprint.Of([]fingerprint.Entry{entries[1], entries[0]})
	if err != nil {
		t.Fatalf("Of reordered: %v", err)
	}
	if reordered != got {
		t.Fatalf("Of reordered = %q, want %q", reordered, got)
	}

	subsecond := append([]fingerprint.Entry(nil), entries...)
	subsecond[0].ModTime = subsecond[0].ModTime.Add(99 * time.Millisecond)
	gotSubsecond, err := fingerprint.Of(subsecond)
	if err != nil {
		t.Fatalf("Of subsecond change: %v", err)
	}
	if gotSubsecond != got {
		t.Fatalf("Of subsecond change = %q, want %q", gotSubsecond, got)
	}

	oneSecond := append([]fingerprint.Entry(nil), entries...)
	oneSecond[0].ModTime = oneSecond[0].ModTime.Add(time.Second)
	gotOneSecond, err := fingerprint.Of(oneSecond)
	if err != nil {
		t.Fatalf("Of one-second change: %v", err)
	}
	if gotOneSecond == got {
		t.Fatalf("Of one-second change = %q, want a different fingerprint", gotOneSecond)
	}
}

func TestOfNormalizesPathsToNFC(t *testing.T) {
	modTime := time.Unix(10, 0)
	nfc, err := fingerprint.Of([]fingerprint.Entry{
		{Path: "Café.ass", Size: 4, ModTime: modTime},
	})
	if err != nil {
		t.Fatalf("Of NFC: %v", err)
	}
	nfd, err := fingerprint.Of([]fingerprint.Entry{
		{Path: "Cafe\u0301.ass", Size: 4, ModTime: modTime},
	})
	if err != nil {
		t.Fatalf("Of NFD: %v", err)
	}
	if nfd != nfc {
		t.Fatalf("Of NFD = %q, want NFC fingerprint %q", nfd, nfc)
	}
}

func TestOfValidation(t *testing.T) {
	valid := fingerprint.Entry{
		Path:    "Season 1/Show.mkv",
		Size:    12,
		ModTime: time.Unix(10, 0),
	}
	cases := []struct {
		name   string
		mutate func(*fingerprint.Entry)
		want   string
	}{
		{
			name:   "path required",
			mutate: func(entry *fingerprint.Entry) { entry.Path = "" },
			want:   "fingerprint: path is required",
		},
		{
			name:   "path relative",
			mutate: func(entry *fingerprint.Entry) { entry.Path = "/Show.mkv" },
			want:   `fingerprint: path "/Show.mkv" must be relative`,
		},
		{
			name:   "path traversal",
			mutate: func(entry *fingerprint.Entry) { entry.Path = "../Show.mkv" },
			want:   `fingerprint: path "../Show.mkv" must not contain ..`,
		},
		{
			name:   "path canonical",
			mutate: func(entry *fingerprint.Entry) { entry.Path = "Season 1//Show.mkv" },
			want:   `fingerprint: path "Season 1//Show.mkv" is not canonical`,
		},
		{
			name:   "path NUL",
			mutate: func(entry *fingerprint.Entry) { entry.Path = "bad\x00path" },
			want:   "fingerprint: path \"bad\\x00path\" must not contain NUL or newline",
		},
		{
			name:   "path newline",
			mutate: func(entry *fingerprint.Entry) { entry.Path = "bad\npath" },
			want:   "fingerprint: path \"bad\\npath\" must not contain NUL or newline",
		},
		{
			name:   "size nonnegative",
			mutate: func(entry *fingerprint.Entry) { entry.Size = -1 },
			want:   `fingerprint: size for "Season 1/Show.mkv" must not be negative`,
		},
		{
			name:   "mtime required",
			mutate: func(entry *fingerprint.Entry) { entry.ModTime = time.Time{} },
			want:   `fingerprint: mtime for "Season 1/Show.mkv" is required`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := valid
			tc.mutate(&entry)
			_, err := fingerprint.Of([]fingerprint.Entry{entry})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Of error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestOfRejectsAmbiguousRecordFraming(t *testing.T) {
	valid := []fingerprint.Entry{
		{Path: "a", Size: 1, ModTime: time.Unix(2, 0)},
		{Path: "b", Size: 3, ModTime: time.Unix(4, 0)},
	}
	if _, err := fingerprint.Of(valid); err != nil {
		t.Fatalf("Of valid collision side: %v", err)
	}

	ambiguous := []fingerprint.Entry{
		{Path: "a\x001\x002\nb", Size: 3, ModTime: time.Unix(4, 0)},
	}
	_, err := fingerprint.Of(ambiguous)
	const want = "fingerprint: path \"a\\x001\\x002\\nb\" must not contain NUL or newline"
	if err == nil || err.Error() != want {
		t.Fatalf("Of ambiguous collision side error = %v, want %q", err, want)
	}
}

func TestOfRejectsInvalidUTF8Path(t *testing.T) {
	_, err := fingerprint.Of([]fingerprint.Entry{{
		Path:    "bad\xffpath.mkv",
		Size:    1,
		ModTime: time.Unix(2, 0),
	}})
	const want = "fingerprint: path \"bad\\xffpath.mkv\" is not valid UTF-8"
	if err == nil || err.Error() != want {
		t.Fatalf("Of error = %v, want %q", err, want)
	}
}

func TestOfRejectsDuplicateCanonicalPaths(t *testing.T) {
	entries := []fingerprint.Entry{
		{Path: "Café.ass", Size: 4, ModTime: time.Unix(10, 0)},
		{Path: "Cafe\u0301.ass", Size: 4, ModTime: time.Unix(10, 0)},
	}
	_, err := fingerprint.Of(entries)
	want := `fingerprint: duplicate path "Café.ass"`
	if err == nil || err.Error() != want {
		t.Fatalf("Of error = %v, want %q", err, want)
	}
}

func TestCompute(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Season 1"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatalf("Mkdir empty: %v", err)
	}
	files := []struct {
		path    string
		content string
		modTime time.Time
	}{
		{path: ".kura.json", content: "meta", modTime: time.Unix(10, 100_000_000)},
		{
			path:    filepath.Join("Season 1", "Show.mkv"),
			content: strings.Repeat("x", 12),
			modTime: time.Unix(20, 900_000_000),
		},
	}
	entries := make([]fingerprint.Entry, 0, len(files))
	for _, file := range files {
		fullPath := filepath.Join(root, file.path)
		if err := os.WriteFile(fullPath, []byte(file.content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", file.path, err)
		}
		if err := os.Chtimes(fullPath, file.modTime, file.modTime); err != nil {
			t.Fatalf("Chtimes(%q): %v", file.path, err)
		}
		entries = append(entries, fingerprint.Entry{
			Path:    filepath.ToSlash(file.path),
			Size:    int64(len(file.content)),
			ModTime: file.modTime,
		})
	}

	want, err := fingerprint.Of(entries)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	got, err := fingerprint.Compute(root)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got != want {
		t.Fatalf("Compute = %q, want %q", got, want)
	}
}

func TestExcludingKuraScopesMatchForWalkAndEntries(t *testing.T) {
	root := t.TempDir()
	files := []struct {
		path    string
		content string
		modTime time.Time
	}{
		{
			path:    filepath.Join(".kura", "series.json"),
			content: `{"generation":8}`,
			modTime: time.Unix(10, 0),
		},
		{
			path:    filepath.Join(".kura", "reconcile", "plan.jsonl"),
			content: "ignored",
			modTime: time.Unix(20, 0),
		},
		{
			path:    filepath.Join("Season 1", "Show.mkv"),
			content: "payload",
			modTime: time.Unix(30, 0),
		},
		{
			path:    filepath.Join("Season 1", ".kura", "kept.txt"),
			content: "nested name is payload",
			modTime: time.Unix(40, 0),
		},
	}
	entries := make([]fingerprint.Entry, 0, len(files))
	for _, file := range files {
		fullPath := filepath.Join(root, file.path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", file.path, err)
		}
		if err := os.WriteFile(fullPath, []byte(file.content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", file.path, err)
		}
		if err := os.Chtimes(fullPath, file.modTime, file.modTime); err != nil {
			t.Fatalf("Chtimes(%q): %v", file.path, err)
		}
		entries = append(entries, fingerprint.Entry{
			Path:    filepath.ToSlash(file.path),
			Size:    int64(len(file.content)),
			ModTime: file.modTime,
		})
	}

	fromEntries, err := fingerprint.OfExcludingKura(entries)
	if err != nil {
		t.Fatalf("OfExcludingKura: %v", err)
	}
	fromWalk, err := fingerprint.ComputeExcludingKura(root)
	if err != nil {
		t.Fatalf("ComputeExcludingKura: %v", err)
	}
	if fromWalk != fromEntries {
		t.Fatalf(
			"ComputeExcludingKura = %q, want entries fingerprint %q",
			fromWalk,
			fromEntries,
		)
	}

	want, err := fingerprint.Of(entries[2:])
	if err != nil {
		t.Fatalf("Of payload entries: %v", err)
	}
	if fromWalk != want {
		t.Fatalf("excluding fingerprint = %q, want payload-only %q", fromWalk, want)
	}
}

func TestComputeRejectsRegularFileRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "series.mkv")
	if err := os.WriteFile(root, []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := fingerprint.Compute(root)
	const want = "fingerprint: root must be a directory"
	if err == nil || err.Error() != want {
		t.Fatalf("Compute error = %v, want %q", err, want)
	}
}

func TestComputeRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	_, err := fingerprint.Compute(root)
	want := `fingerprint: symlink "link" is not supported`
	if err == nil || err.Error() != want {
		t.Fatalf("Compute error = %v, want %q", err, want)
	}
	if !errors.Is(err, fingerprint.ErrSymlink) {
		t.Fatalf("Compute error = %v, want ErrSymlink", err)
	}
}

func TestComputeRejectsNonRegularFile(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "fifo"), 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}

	_, err := fingerprint.Compute(root)
	const want = `fingerprint: non-regular file "fifo" is not supported`
	if err == nil || err.Error() != want {
		t.Fatalf("Compute error = %v, want %q", err, want)
	}
	if !errors.Is(err, fingerprint.ErrNonRegularFile) {
		t.Fatalf("Compute error = %v, want ErrNonRegularFile", err)
	}
}

func TestComputeRejectsSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("Mkdir root: %v", err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	_, err := fingerprint.Compute(link)
	want := "fingerprint: root must not be a symlink"
	if err == nil || err.Error() != want {
		t.Fatalf("Compute error = %v, want %q", err, want)
	}
}

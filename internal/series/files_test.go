package series

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSeriesDirRejectsEscapingRelPath(t *testing.T) {
	seriesDir, err := ParseSeriesDir(t.TempDir())
	if err != nil {
		t.Fatalf("ParseSeriesDir: %v", err)
	}
	if _, err := seriesDir.JoinRel("../episode.mkv"); err == nil {
		t.Fatal("JoinRel returned nil error, want escaping path rejection")
	}
	if _, err := seriesDir.JoinRel(".kura/series.json"); err == nil {
		t.Fatal("JoinRel returned nil error, want .kura path rejection")
	}
}

func TestFileStatTruncatesMTimeToStoredPrecision(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "episode.mkv")
	if err := os.WriteFile(path, []byte("episode"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	mtime := time.Date(2025, 6, 23, 5, 31, 53, 122999907, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	facts, err := files{}.stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if want := mtime.Truncate(time.Second); !facts.MTime.Equal(want) {
		t.Fatalf("MTime = %s, want %s", facts.MTime.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}

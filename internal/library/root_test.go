package library

import (
	"path/filepath"
	"testing"
)

func TestRootJoin(t *testing.T) {
	root, err := ParseRoot(t.TempDir())
	if err != nil {
		t.Fatalf("ParseRoot: %v", err)
	}
	if got, want := root.Join("Bookworm", "Season 1"), filepath.Join(root.Path(), "Bookworm", "Season 1"); got != want {
		t.Fatalf("Join = %q, want %q", got, want)
	}
}

package paths_test

import (
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/paths"
)

func TestTapeCatalogPaths(t *testing.T) {
	stateRoot := "/var/lib/kura/backup"
	if got, want := paths.TapeCatalogDir(stateRoot), filepath.Join(stateRoot, "tapes"); got != want {
		t.Fatalf("TapeCatalogDir() = %q, want %q", got, want)
	}
	if got, want := paths.TapeCatalog(stateRoot, "ABC123L6"), filepath.Join(stateRoot, "tapes", "ABC123L6.json"); got != want {
		t.Fatalf("TapeCatalog() = %q, want %q", got, want)
	}
}

package paths_test

import (
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/paths"
)

func TestVolumeCatalogDirectories(t *testing.T) {
	stateRoot := "/var/lib/kura/backup"

	if got, want := paths.ActiveVolumeCatalogDir(stateRoot), filepath.Join(stateRoot, "volumes", "active"); got != want {
		t.Fatalf("ActiveVolumeCatalogDir() = %q, want %q", got, want)
	}
	if got, want := paths.DetachedVolumeCatalogDir(stateRoot), filepath.Join(stateRoot, "volumes", "detached"); got != want {
		t.Fatalf("DetachedVolumeCatalogDir() = %q, want %q", got, want)
	}
}

package paths_test

import (
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/paths"
)

func TestVolumeCatalogPaths(t *testing.T) {
	stateRoot := "/var/lib/kura/backup"
	volumeID := "01J8ZQ7W5TWHA6R6J8X4QZ9Y7V"

	if got, want := paths.ActiveVolumeCatalogDir(stateRoot), filepath.Join(stateRoot, "volumes", "active"); got != want {
		t.Fatalf("ActiveVolumeCatalogDir() = %q, want %q", got, want)
	}
	if got, want := paths.ActiveVolumeCatalog(stateRoot, volumeID), filepath.Join(stateRoot, "volumes", "active", volumeID+".json"); got != want {
		t.Fatalf("ActiveVolumeCatalog() = %q, want %q", got, want)
	}
	if got, want := paths.DetachedVolumeCatalogDir(stateRoot), filepath.Join(stateRoot, "volumes", "detached"); got != want {
		t.Fatalf("DetachedVolumeCatalogDir() = %q, want %q", got, want)
	}
	if got, want := paths.DetachedVolumeCatalog(stateRoot, volumeID), filepath.Join(stateRoot, "volumes", "detached", volumeID+".json"); got != want {
		t.Fatalf("DetachedVolumeCatalog() = %q, want %q", got, want)
	}
}

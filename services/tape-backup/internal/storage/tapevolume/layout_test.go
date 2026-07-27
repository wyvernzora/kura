package tapevolume_test

import (
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
)

func TestLayoutPaths(t *testing.T) {
	root := filepath.Join("mnt", "ltfs")
	if got, want := tapevolume.ArchiveDir(root), filepath.Join(root, "KURA_ARCHIVE"); got != want {
		t.Fatalf("ArchiveDir = %q, want %q", got, want)
	}
	if got, want := tapevolume.VolumeFile(root), filepath.Join(root, "KURA_ARCHIVE", "volume.json"); got != want {
		t.Fatalf("VolumeFile = %q, want %q", got, want)
	}
	if got, want := tapevolume.SnapshotsDir(root), filepath.Join(root, "KURA_ARCHIVE", "snapshots"); got != want {
		t.Fatalf("SnapshotsDir = %q, want %q", got, want)
	}

	name, err := tapevolume.SnapshotName("tvdb:370070", 7)
	if err != nil {
		t.Fatalf("SnapshotName: %v", err)
	}
	if want := "OR3GIYR2GM3TAMBXGA.g7"; name != want {
		t.Fatalf("SnapshotName = %q, want %q", name, want)
	}
	got, err := tapevolume.SnapshotDir(root, "tvdb:370070", 7)
	if err != nil {
		t.Fatalf("SnapshotDir: %v", err)
	}
	want := filepath.Join(root, "KURA_ARCHIVE", "snapshots", name)
	if got != want {
		t.Fatalf("SnapshotDir = %q, want %q", got, want)
	}
}

func TestSnapshotNameWrapsFormatError(t *testing.T) {
	_, err := tapevolume.SnapshotName("", 1)
	const want = "tapevolume: metadataRef is required"
	if err == nil || err.Error() != want {
		t.Fatalf("SnapshotName error = %v, want %q", err, want)
	}
}

func TestParseSnapshotNameWrapsParseError(t *testing.T) {
	_, _, err := tapevolume.ParseSnapshotName("invalid")
	const want = `tapevolume: snapshot name "invalid" is missing .g<generation>`
	if err == nil || err.Error() != want {
		t.Fatalf("ParseSnapshotName error = %v, want %q", err, want)
	}
}

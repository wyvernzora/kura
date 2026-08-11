package tapevolume

import (
	"fmt"
	"path/filepath"

	"github.com/wyvernzora/kura/services/tape-backup/internal/snapshotname"
)

const archiveDirectory = "KURA_ARCHIVE"

// ArchiveDir returns the Kura archive directory on an LTFS volume.
func ArchiveDir(ltfsRoot string) string {
	return filepath.Join(ltfsRoot, archiveDirectory)
}

// VolumeFile returns the volume header path on an LTFS volume.
func VolumeFile(ltfsRoot string) string {
	return filepath.Join(ArchiveDir(ltfsRoot), "volume.json")
}

// SnapshotsDir returns the snapshot directory on an LTFS volume.
func SnapshotsDir(ltfsRoot string) string {
	return filepath.Join(ArchiveDir(ltfsRoot), "snapshots")
}

// SnapshotDir returns the directory for one series generation.
func SnapshotDir(ltfsRoot, metadataRef string, generation int) (string, error) {
	name, err := SnapshotName(metadataRef, generation)
	if err != nil {
		return "", err
	}
	return filepath.Join(SnapshotsDir(ltfsRoot), name), nil
}

// SnapshotName returns the permanent on-tape name for one series generation.
//
// This format is permanent: changing it would orphan every snapshot already
// written to tape.
func SnapshotName(metadataRef string, generation int) (string, error) {
	name, err := snapshotname.Format(metadataRef, generation)
	if err != nil {
		return "", fmt.Errorf("tapevolume: %w", err)
	}
	return name, nil
}

// ParseSnapshotName decodes one canonical on-tape snapshot name.
func ParseSnapshotName(name string) (metadataRef string, generation int, err error) {
	metadataRef, generation, err = snapshotname.Parse(name)
	if err != nil {
		return "", 0, fmt.Errorf("tapevolume: %w", err)
	}
	return metadataRef, generation, nil
}

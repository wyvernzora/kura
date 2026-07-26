package tapevolume

import (
	"encoding/base32"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	archiveDirectory     = "KURA_ARCHIVE"
	maxSnapshotNameBytes = 255
)

var snapshotEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

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
// Base32 is a reversible bijection over the raw metadata reference bytes, so
// distinct references cannot collide. This format is permanent: changing it
// would orphan every snapshot already written to tape.
func SnapshotName(metadataRef string, generation int) (string, error) {
	if metadataRef == "" {
		return "", errors.New("tapevolume: metadataRef is required")
	}
	if generation < 1 {
		return "", errors.New("tapevolume: generation must be at least 1")
	}
	encodedRef := snapshotEncoding.EncodeToString([]byte(metadataRef))
	name := encodedRef + ".g" + strconv.Itoa(generation)
	if len(name) > maxSnapshotNameBytes {
		return "", fmt.Errorf(
			"tapevolume: snapshot name exceeds %d-byte limit",
			maxSnapshotNameBytes,
		)
	}
	return name, nil
}

// ParseSnapshotName decodes one canonical on-tape snapshot name.
func ParseSnapshotName(name string) (metadataRef string, generation int, err error) {
	separator := strings.LastIndexByte(name, '.')
	if separator < 1 || separator == len(name)-1 || name[separator+1] != 'g' {
		return "", 0, fmt.Errorf(
			"tapevolume: snapshot name %q is missing .g<generation>",
			name,
		)
	}

	encodedRef := name[:separator]
	generationText := name[separator+2:]
	parsedGeneration, err := strconv.ParseUint(generationText, 10, strconv.IntSize)
	if err != nil || strconv.FormatUint(parsedGeneration, 10) != generationText {
		return "", 0, fmt.Errorf(
			"tapevolume: snapshot name %q has invalid generation %q",
			name,
			generationText,
		)
	}
	generation = int(parsedGeneration)
	if generation < 1 {
		return "", 0, fmt.Errorf(
			"tapevolume: snapshot name %q generation must be at least 1",
			name,
		)
	}

	decodedRef, err := snapshotEncoding.DecodeString(encodedRef)
	if err != nil {
		return "", 0, fmt.Errorf(
			"tapevolume: snapshot name %q has invalid base32 metadataRef: %w",
			name,
			err,
		)
	}
	if len(decodedRef) == 0 {
		return "", 0, errors.New("tapevolume: metadataRef is required")
	}
	if snapshotEncoding.EncodeToString(decodedRef) != encodedRef {
		return "", 0, fmt.Errorf(
			"tapevolume: snapshot name %q is not canonical",
			name,
		)
	}
	return string(decodedRef), generation, nil
}

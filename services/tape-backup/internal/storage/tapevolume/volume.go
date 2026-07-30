// Package tapevolume owns the LTFS-visible Kura archive volume header and
// snapshot layout.
package tapevolume

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/renameio/v2/maybe"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

const schemaVersion = 1

// Volume identifies one initialized Kura archive volume.
type Volume struct {
	// VolumeID identifies both the archived content and its initialization:
	// clones keep it, while reformatting mints a new one. A separate format
	// counter would carry no additional information.
	VolumeID       volume.ID
	TapeID         tape.ID
	KeyFingerprint string
	CreatedAt      time.Time
}

type volumeWire struct {
	SchemaVersion  int    `json:"schemaVersion"`
	VolumeID       string `json:"volumeID"`
	TapeID         string `json:"tapeID"`
	KeyFingerprint string `json:"keyFingerprint"`
	CreatedAt      string `json:"createdAt"`
}

// Write validates and atomically writes the volume header.
func Write(ltfsRoot string, volume Volume) error {
	if err := validateVolume(volume); err != nil {
		return err
	}
	data, err := json.MarshalIndent(toWire(volume), "", "  ")
	if err != nil {
		return fmt.Errorf("tapevolume: encode: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(ArchiveDir(ltfsRoot), 0o775); err != nil {
		return fmt.Errorf("tapevolume: create archive directory: %w", err)
	}
	if err := maybe.WriteFile(VolumeFile(ltfsRoot), data, 0o664); err != nil {
		return fmt.Errorf("tapevolume: write: %w", err)
	}
	return nil
}

// Read reads and validates the volume header. A missing header wraps
// os.ErrNotExist.
func Read(ltfsRoot string) (Volume, error) {
	data, err := os.ReadFile(VolumeFile(ltfsRoot))
	if err != nil {
		return Volume{}, fmt.Errorf("tapevolume: read: %w", err)
	}
	var wire volumeWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return Volume{}, fmt.Errorf("tapevolume: decode: %w", err)
	}
	if wire.SchemaVersion != schemaVersion {
		return Volume{}, fmt.Errorf(
			"tapevolume: unsupported schemaVersion %d",
			wire.SchemaVersion,
		)
	}
	volume, err := fromWire(wire)
	if err != nil {
		return Volume{}, err
	}
	if err := validateVolume(volume); err != nil {
		return Volume{}, err
	}
	return volume, nil
}

func validateVolume(volume Volume) error {
	if _, err := volumeID(volume.VolumeID); err != nil {
		return err
	}
	if _, err := tape.ParseID(string(volume.TapeID)); err != nil {
		return fmt.Errorf("tapevolume: %w", err)
	}
	if len(volume.KeyFingerprint) != 16 || !lowerHex(volume.KeyFingerprint) {
		return errors.New(
			"tapevolume: keyFingerprint must be 16 lowercase hexadecimal characters",
		)
	}
	if volume.CreatedAt.IsZero() {
		return errors.New("tapevolume: createdAt is required")
	}
	if _, offset := volume.CreatedAt.Zone(); offset != 0 {
		return errors.New("tapevolume: createdAt must be UTC")
	}
	return nil
}

func toWire(volume Volume) volumeWire {
	return volumeWire{
		SchemaVersion:  schemaVersion,
		VolumeID:       string(volume.VolumeID),
		TapeID:         string(volume.TapeID),
		KeyFingerprint: volume.KeyFingerprint,
		CreatedAt:      volume.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func fromWire(wire volumeWire) (Volume, error) {
	createdAt, err := time.Parse(time.RFC3339, wire.CreatedAt)
	if err != nil {
		return Volume{}, fmt.Errorf("tapevolume: parse createdAt: %w", err)
	}
	return Volume{
		VolumeID:       volume.ID(wire.VolumeID),
		TapeID:         tape.ID(wire.TapeID),
		KeyFingerprint: wire.KeyFingerprint,
		CreatedAt:      createdAt,
	}, nil
}

func lowerHex(value string) bool {
	for _, character := range value {
		if character >= '0' && character <= '9' {
			continue
		}
		if character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func volumeID(id volume.ID) (volume.ID, error) {
	parsed, err := volume.ParseID(string(id))
	if err != nil {
		return "", fmt.Errorf("tapevolume: %w", err)
	}
	return parsed, nil
}

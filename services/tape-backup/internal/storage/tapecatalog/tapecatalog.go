// Package tapecatalog owns rebuildable mirrors of observed archive volumes.
package tapecatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/renameio/v2/maybe"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/paths"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapemanifest"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

const schemaVersion = 1

// Cross-process races are deliberately not defended: one server owns the cache.
var mutationMu sync.Mutex

// Observed records facts learned from the mounted filesystem.
type Observed struct {
	TapeID         tape.ID
	ObservedAt     time.Time
	CapacityBytes  int64
	FreeBytes      int64
	LastVerifiedAt time.Time
}

type observedWire struct {
	SchemaVersion  int    `json:"schemaVersion"`
	TapeID         string `json:"tapeID"`
	ObservedAt     string `json:"observedAt"`
	CapacityBytes  int64  `json:"capacityBytes"`
	FreeBytes      int64  `json:"freeBytes"`
	LastVerifiedAt string `json:"lastVerifiedAt,omitempty"`
}

// VolumeDir returns the active mirror, otherwise the detached mirror.
// The returned path is a point-in-time answer; a concurrent move may invalidate it.
func VolumeDir(stateRoot string, id volume.ID) (string, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	return volumeDir(stateRoot, id)
}

func volumeDir(stateRoot string, id volume.ID) (string, error) {
	if err := validateVolumeID(id); err != nil {
		return "", err
	}
	return findVolume(stateRoot, id, false)
}

// SaveObserved atomically rewrites observed.json.
func SaveObserved(stateRoot string, id volume.ID, observed Observed) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := validateVolumeID(id); err != nil {
		return err
	}
	if err := validateObserved(observed); err != nil {
		return err
	}
	data, err := json.MarshalIndent(toWire(observed), "", "  ")
	if err != nil {
		return fmt.Errorf("tapecatalog: encode observed %s: %w", id, err)
	}
	data = append(data, '\n')
	return mutateVolume(stateRoot, id, func(volumeDir string) error {
		return writeObserved(volumeDir, id, data)
	})
}

// LoadObserved reads and validates observed.json.
func LoadObserved(stateRoot string, id volume.ID) (Observed, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	volumeDir, err := volumeDir(stateRoot, id)
	if err != nil {
		return Observed{}, err
	}
	data, err := os.ReadFile(observedFile(volumeDir))
	if err != nil {
		return Observed{}, fmt.Errorf("tapecatalog: read observed %s: %w", id, err)
	}
	var wire observedWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return Observed{}, fmt.Errorf("tapecatalog: decode observed %s: %w", id, err)
	}
	if wire.SchemaVersion != schemaVersion {
		return Observed{}, fmt.Errorf(
			"tapecatalog: unsupported observed schemaVersion %d",
			wire.SchemaVersion,
		)
	}
	observed, err := fromWire(wire)
	if err != nil {
		return Observed{}, err
	}
	if err := validateObserved(observed); err != nil {
		return Observed{}, err
	}
	return observed, nil
}

// PutVolumeHeader validates and stores volume.json verbatim.
func PutVolumeHeader(stateRoot string, id volume.ID, header []byte) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := validateVolumeID(id); err != nil {
		return err
	}
	return mutateVolume(stateRoot, id, func(volumeDir string) error {
		return putVolumeHeader(volumeDir, id, header)
	})
}

// PutSnapshot validates and stores complete or incomplete snapshot bytes.
func PutSnapshot(stateRoot string, id volume.ID, name string, manifest, complete []byte) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := validateVolumeID(id); err != nil {
		return err
	}
	if _, _, err := tapevolume.ParseSnapshotName(name); err != nil {
		return fmt.Errorf("tapecatalog: put snapshot %s: %w", name, err)
	}
	return mutateVolume(stateRoot, id, func(volumeDir string) error {
		return putSnapshot(volumeDir, name, manifest, complete)
	})
}

// ListActive returns a point-in-time snapshot of the active directory.
// Composing it with ListDetached is not atomic; a consistent view of both sets
// must take mutationMu or use a combined accessor.
func ListActive(stateRoot string) ([]volume.ID, error) {
	return list(paths.ActiveVolumeCatalogDir(stateRoot), "active")
}

// ListDetached returns a point-in-time snapshot of the detached directory.
// Composing it with ListActive is not atomic; a consistent view of both sets
// must take mutationMu or use a combined accessor.
func ListDetached(stateRoot string) ([]volume.ID, error) {
	return list(paths.DetachedVolumeCatalogDir(stateRoot), "detached")
}

// Detach atomically moves a volume mirror from active to detached.
func Detach(stateRoot string, id volume.ID) error {
	return move(id, "detach", "active", "detached", activeVolumeDir(stateRoot, id),
		detachedVolumeDir(stateRoot, id), paths.DetachedVolumeCatalogDir(stateRoot))
}

// Attach atomically moves a volume mirror from detached to active.
func Attach(stateRoot string, id volume.ID) error {
	return move(id, "attach", "detached", "active", detachedVolumeDir(stateRoot, id),
		activeVolumeDir(stateRoot, id), paths.ActiveVolumeCatalogDir(stateRoot))
}

// Purge removes a volume mirror from both sets.
func Purge(stateRoot string, id volume.ID) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := validateVolumeID(id); err != nil {
		return err
	}
	var purgeErrors []error
	for _, path := range []string{activeVolumeDir(stateRoot, id), detachedVolumeDir(stateRoot, id)} {
		if err := os.RemoveAll(path); err != nil {
			purgeErrors = append(purgeErrors, fmt.Errorf("tapecatalog: purge %s at %q: %w", id, path, err))
		}
	}
	return errors.Join(purgeErrors...)
}

func findVolume(stateRoot string, id volume.ID, requireDisjoint bool) (string, error) {
	active := activeVolumeDir(stateRoot, id)
	detached := detachedVolumeDir(stateRoot, id)
	activeExists, err := directoryExists(active)
	if err != nil {
		return "", fmt.Errorf("tapecatalog: inspect active volume %s: %w", id, err)
	}
	if activeExists && !requireDisjoint {
		return active, nil
	}
	detachedExists, err := directoryExists(detached)
	if err != nil {
		return "", fmt.Errorf("tapecatalog: inspect detached volume %s: %w", id, err)
	}
	if activeExists && detachedExists {
		return "", fmt.Errorf("tapecatalog: volume %s exists at both %q and %q", id, active, detached)
	}
	if activeExists {
		return active, nil
	}
	if detachedExists {
		return detached, nil
	}
	if !requireDisjoint {
		return "", fmt.Errorf("tapecatalog: volume %s does not exist: %w", id, os.ErrNotExist)
	}
	return "", nil
}

func directoryExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%q is not a directory", path)
	}
	return true, nil
}

func createActiveVolume(stateRoot string, id volume.ID, populate func(string) error) (resultErr error) {
	parent := paths.ActiveVolumeCatalogDir(stateRoot)
	if err := os.MkdirAll(parent, 0o775); err != nil {
		return fmt.Errorf("tapecatalog: create active directory: %w", err)
	}
	tempDir, err := os.MkdirTemp(parent, ".volume-")
	if err != nil {
		return fmt.Errorf("tapecatalog: create temporary volume: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, cleanupTemp(tempDir, "volume"))
	}()
	if err := os.Chmod(tempDir, 0o775); err != nil {
		return fmt.Errorf("tapecatalog: set temporary volume permissions: %w", err)
	}
	if err := populate(tempDir); err != nil {
		return err
	}

	destination := activeVolumeDir(stateRoot, id)
	sibling := detachedVolumeDir(stateRoot, id)
	// findVolume checked both paths under mutationMu. These repeated checks
	// improve the error if another process raced that check; they do not make
	// cross-process use safe. os.Rename would also refuse an existing destination
	// directory; that check is belt-and-braces for error quality.
	if exists, err := directoryExists(destination); err != nil {
		return fmt.Errorf("tapecatalog: inspect new volume destination: %w", err)
	} else if exists {
		return fmt.Errorf("tapecatalog: create volume %s: destination %q already exists", id, destination)
	}
	if exists, err := directoryExists(sibling); err != nil {
		return fmt.Errorf("tapecatalog: inspect new volume sibling: %w", err)
	} else if exists {
		return fmt.Errorf("tapecatalog: create volume %s: sibling %q already exists", id, sibling)
	}
	if err := os.Rename(tempDir, destination); err != nil {
		return fmt.Errorf("tapecatalog: create volume %s: rename: %w", id, err)
	}
	return nil
}

func mutateVolume(stateRoot string, id volume.ID, mutate func(string) error) error {
	volumeDir, err := findVolume(stateRoot, id, true)
	if err != nil {
		return err
	}
	if volumeDir != "" {
		return mutate(volumeDir)
	}
	return createActiveVolume(stateRoot, id, mutate)
}

func writeObserved(volumeDir string, id volume.ID, data []byte) error {
	if err := maybe.WriteFile(observedFile(volumeDir), data, 0o664); err != nil {
		return fmt.Errorf("tapecatalog: write observed %s: %w", id, err)
	}
	return nil
}

func putVolumeHeader(volumeDir string, id volume.ID, header []byte) (resultErr error) {
	tempRoot, err := os.MkdirTemp(volumeDir, ".header-")
	if err != nil {
		return fmt.Errorf("tapecatalog: create temporary volume header: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, cleanupTemp(tempRoot, "volume header"))
	}()
	tempFile := tapevolume.VolumeFile(tempRoot)
	if err := os.MkdirAll(filepath.Dir(tempFile), 0o775); err != nil {
		return fmt.Errorf("tapecatalog: create temporary archive directory: %w", err)
	}
	if err := os.WriteFile(tempFile, header, 0o664); err != nil {
		return fmt.Errorf("tapecatalog: write temporary volume header: %w", err)
	}
	decoded, err := tapevolume.Read(tempRoot)
	if err != nil {
		return fmt.Errorf("tapecatalog: put volume header %s: %w", id, err)
	}
	if decoded.VolumeID != id {
		return fmt.Errorf("tapecatalog: volumeID mismatch: catalog is %q, header contains %q",
			id, decoded.VolumeID)
	}
	destination := tapevolume.VolumeFile(volumeDir)
	if err := os.MkdirAll(filepath.Dir(destination), 0o775); err != nil {
		return fmt.Errorf("tapecatalog: create archive directory: %w", err)
	}
	if err := os.Rename(tempFile, destination); err != nil {
		return fmt.Errorf("tapecatalog: store volume header %s: %w", id, err)
	}
	return nil
}

func putSnapshot(volumeDir, name string, manifest, complete []byte) (resultErr error) {
	snapshotsDir := tapevolume.SnapshotsDir(volumeDir)
	if err := os.MkdirAll(snapshotsDir, 0o775); err != nil {
		return fmt.Errorf("tapecatalog: create snapshots directory: %w", err)
	}
	tempParent, err := os.MkdirTemp(volumeDir, ".snapshot-")
	if err != nil {
		return fmt.Errorf("tapecatalog: create temporary snapshot: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, cleanupTemp(tempParent, "snapshot"))
	}()
	tempSnapshot := filepath.Join(tempParent, name)
	if err := os.Mkdir(tempSnapshot, 0o775); err != nil {
		return fmt.Errorf("tapecatalog: create temporary snapshot directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tempSnapshot, "manifest.json"), manifest, 0o664); err != nil {
		return fmt.Errorf("tapecatalog: write temporary manifest: %w", err)
	}
	if complete != nil {
		if err := os.WriteFile(filepath.Join(tempSnapshot, "complete.json"), complete, 0o664); err != nil {
			return fmt.Errorf("tapecatalog: write temporary complete marker: %w", err)
		}
		if _, err := tapemanifest.Read(tempSnapshot); err != nil {
			return fmt.Errorf("tapecatalog: put snapshot %s: %w", name, err)
		}
	} else if _, err := tapemanifest.ReadManifest(tempSnapshot); err != nil {
		return fmt.Errorf("tapecatalog: put snapshot %s: %w", name, err)
	}
	destination := filepath.Join(snapshotsDir, name)
	if err := prepareSnapshotDestination(name, destination, complete); err != nil {
		return err
	}
	if err := os.Rename(tempSnapshot, destination); err != nil {
		return fmt.Errorf("tapecatalog: put snapshot %s: rename: %w", name, err)
	}
	return nil
}

func prepareSnapshotDestination(name, destination string, complete []byte) error {
	_, destinationErr := os.Lstat(destination)
	switch {
	case errors.Is(destinationErr, os.ErrNotExist):
	case destinationErr != nil:
		return fmt.Errorf(
			"tapecatalog: put snapshot %s: inspect destination: %w",
			name,
			destinationErr,
		)
	default:
		_, readErr := tapemanifest.Read(destination)
		if readErr == nil || complete == nil {
			return fmt.Errorf("tapecatalog: put snapshot %s: destination %q already exists", name, destination)
		}
		if !errors.Is(readErr, tapemanifest.ErrIncomplete) {
			return fmt.Errorf("tapecatalog: put snapshot %s: inspect destination: %w", name, readErr)
		}
		if err := os.RemoveAll(destination); err != nil {
			return fmt.Errorf("tapecatalog: put snapshot %s: remove incomplete destination: %w", name, err)
		}
	}
	return nil
}

func list(dir, set string) ([]volume.ID, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []volume.ID{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tapecatalog: list %s: %w", set, err)
	}
	ids := make([]volume.ID, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id, err := volume.ParseID(entry.Name())
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func move(id volume.ID, action, sourceSet, destinationSet, source, destination, destinationDir string) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := validateVolumeID(id); err != nil {
		return err
	}
	if exists, err := directoryExists(source); err != nil {
		return fmt.Errorf("tapecatalog: %s %s: inspect source: %w", action, id, err)
	} else if !exists {
		return fmt.Errorf("tapecatalog: %s %s: %s volume does not exist", action, id, sourceSet)
	}
	if err := os.MkdirAll(destinationDir, 0o775); err != nil {
		return fmt.Errorf("tapecatalog: %s %s: create destination directory: %w", action, id, err)
	}
	// os.Rename also refuses an existing directory destination. This
	// belt-and-braces check preserves the clearer catalog error.
	if exists, err := directoryExists(destination); err != nil {
		return fmt.Errorf("tapecatalog: %s %s: inspect destination: %w", action, id, err)
	} else if exists {
		return fmt.Errorf("tapecatalog: %s %s: cannot move from %q to %q: %s volume already exists",
			action, id, source, destination, destinationSet)
	}
	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("tapecatalog: %s %s: rename: %w", action, id, err)
	}
	return nil
}

func validateObserved(observed Observed) error {
	if _, err := tape.ParseID(string(observed.TapeID)); err != nil {
		return fmt.Errorf("tapecatalog: %w", err)
	}
	if observed.ObservedAt.IsZero() {
		return errors.New("tapecatalog: observedAt is required")
	}
	if _, offset := observed.ObservedAt.Zone(); offset != 0 {
		return errors.New("tapecatalog: observedAt must be UTC")
	}
	if observed.CapacityBytes < 0 {
		return errors.New("tapecatalog: capacityBytes must not be negative")
	}
	if observed.FreeBytes < 0 {
		return errors.New("tapecatalog: freeBytes must not be negative")
	}
	if observed.FreeBytes > observed.CapacityBytes {
		return errors.New("tapecatalog: freeBytes must not exceed capacityBytes")
	}
	if !observed.LastVerifiedAt.IsZero() {
		if _, offset := observed.LastVerifiedAt.Zone(); offset != 0 {
			return errors.New("tapecatalog: lastVerifiedAt must be UTC")
		}
	}
	return nil
}

func toWire(observed Observed) observedWire {
	lastVerifiedAt := ""
	if !observed.LastVerifiedAt.IsZero() {
		lastVerifiedAt = observed.LastVerifiedAt.UTC().Format(time.RFC3339)
	}
	return observedWire{
		SchemaVersion:  schemaVersion,
		TapeID:         string(observed.TapeID),
		ObservedAt:     observed.ObservedAt.UTC().Format(time.RFC3339),
		CapacityBytes:  observed.CapacityBytes,
		FreeBytes:      observed.FreeBytes,
		LastVerifiedAt: lastVerifiedAt,
	}
}

func fromWire(wire observedWire) (Observed, error) {
	var observedAt time.Time
	if wire.ObservedAt != "" {
		var err error
		observedAt, err = parseTime("observedAt", wire.ObservedAt)
		if err != nil {
			return Observed{}, err
		}
	}
	var lastVerifiedAt time.Time
	if wire.LastVerifiedAt != "" {
		var err error
		lastVerifiedAt, err = parseTime("lastVerifiedAt", wire.LastVerifiedAt)
		if err != nil {
			return Observed{}, err
		}
	}
	return Observed{
		TapeID:         tape.ID(wire.TapeID),
		ObservedAt:     observedAt,
		CapacityBytes:  wire.CapacityBytes,
		FreeBytes:      wire.FreeBytes,
		LastVerifiedAt: lastVerifiedAt,
	}, nil
}

func parseTime(field, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("tapecatalog: parse %s: %w", field, err)
	}
	return parsed, nil
}

func validateVolumeID(id volume.ID) error {
	if _, err := volume.ParseID(string(id)); err != nil {
		return fmt.Errorf("tapecatalog: %w", err)
	}
	return nil
}

func activeVolumeDir(stateRoot string, id volume.ID) string {
	return filepath.Join(paths.ActiveVolumeCatalogDir(stateRoot), string(id))
}

func detachedVolumeDir(stateRoot string, id volume.ID) string {
	return filepath.Join(paths.DetachedVolumeCatalogDir(stateRoot), string(id))
}

func observedFile(volumeDir string) string {
	return filepath.Join(volumeDir, "observed.json")
}

func cleanupTemp(path, kind string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("tapecatalog: remove temporary %s: %w", kind, err)
	}
	return nil
}

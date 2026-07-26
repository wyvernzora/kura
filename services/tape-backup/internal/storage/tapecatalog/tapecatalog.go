// Package tapecatalog owns the observed per-volume catalogs under
// <state root>/volumes/{active,detached}/<volumeID>.json. Catalogs are
// rebuildable caches of what an agent last observed; the volume remains
// authoritative.
package tapecatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/renameio/v2/maybe"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/paths"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

const schemaVersion = 1

// mutationMu closes in-process interleaving between catalog mutation checks
// and filesystem changes. Cross-process races are deliberately not defended:
// catalogs are rebuildable caches owned by one server process.
var mutationMu sync.Mutex

// Catalog records what the agent last observed in one archive volume.
type Catalog struct {
	VolumeID volume.ID
	// TapeID is the mutable cartridge label last observed for this volume.
	TapeID         tape.ID
	CreatedAt      time.Time
	ObservedAt     time.Time
	CapacityBytes  int64
	FreeBytes      int64
	LastVerifiedAt time.Time
	// Snapshots may be empty: a blank catalog is a registered volume with
	// full free space, and the catalog itself is the volume registry.
	Snapshots []Snapshot
}

// Snapshot identifies one series generation observed in a volume.
type Snapshot struct {
	MetadataRef        string
	Generation         int
	Bytes              int64
	PayloadFingerprint string
	CapturedAt         time.Time
	Incomplete         bool
}

type catalogWire struct {
	SchemaVersion  int            `json:"schemaVersion"`
	VolumeID       string         `json:"volumeID"`
	TapeID         string         `json:"tapeID"`
	CreatedAt      string         `json:"createdAt"`
	ObservedAt     string         `json:"observedAt"`
	CapacityBytes  int64          `json:"capacityBytes"`
	FreeBytes      int64          `json:"freeBytes"`
	LastVerifiedAt string         `json:"lastVerifiedAt,omitempty"`
	Snapshots      []snapshotWire `json:"snapshots"`
}

type snapshotWire struct {
	MetadataRef        string `json:"metadataRef"`
	Generation         int    `json:"generation"`
	Bytes              int64  `json:"bytes"`
	PayloadFingerprint string `json:"payloadFingerprint"`
	CapturedAt         string `json:"capturedAt"`
	Incomplete         bool   `json:"incomplete,omitempty"`
}

// LoadActive reads and validates one active catalog. A missing catalog wraps
// os.ErrNotExist.
func LoadActive(stateRoot string, id volume.ID) (Catalog, error) {
	return load(paths.ActiveVolumeCatalog(stateRoot, string(id)), id)
}

// LoadDetached reads and validates one detached catalog. A missing catalog
// wraps os.ErrNotExist.
func LoadDetached(stateRoot string, id volume.ID) (Catalog, error) {
	return load(paths.DetachedVolumeCatalog(stateRoot, string(id)), id)
}

func load(path string, id volume.ID) (Catalog, error) {
	if err := validateVolumeID(id); err != nil {
		return Catalog{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, fmt.Errorf("tapecatalog: read %s: %w", id, err)
	}
	var wire catalogWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return Catalog{}, fmt.Errorf("tapecatalog: decode %s: %w", id, err)
	}
	if wire.SchemaVersion != schemaVersion {
		return Catalog{}, fmt.Errorf(
			"tapecatalog: unsupported schemaVersion %d",
			wire.SchemaVersion,
		)
	}
	catalog, err := fromWire(wire)
	if err != nil {
		return Catalog{}, err
	}
	if err := validateCatalog(catalog); err != nil {
		return Catalog{}, err
	}
	if catalog.VolumeID != id {
		return Catalog{}, fmt.Errorf(
			"tapecatalog: volumeID mismatch: filename is %q, file contains %q",
			id,
			catalog.VolumeID,
		)
	}
	return catalog, nil
}

// SaveActive validates and atomically rewrites one active catalog.
func SaveActive(stateRoot string, catalog Catalog) error {
	return save(
		paths.ActiveVolumeCatalogDir(stateRoot),
		paths.ActiveVolumeCatalog(stateRoot, string(catalog.VolumeID)),
		paths.DetachedVolumeCatalog(stateRoot, string(catalog.VolumeID)),
		catalog,
	)
}

// SaveDetached validates and atomically rewrites one detached catalog.
func SaveDetached(stateRoot string, catalog Catalog) error {
	return save(
		paths.DetachedVolumeCatalogDir(stateRoot),
		paths.DetachedVolumeCatalog(stateRoot, string(catalog.VolumeID)),
		paths.ActiveVolumeCatalog(stateRoot, string(catalog.VolumeID)),
		catalog,
	)
}

// There is deliberately no compare-and-swap: the server is the sole writer,
// at most one backup plan is active, and this file is a rebuildable cache.
func save(dir, path, siblingPath string, catalog Catalog) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()

	if err := validateCatalog(catalog); err != nil {
		return err
	}
	data, err := json.MarshalIndent(toWire(catalog), "", "  ")
	if err != nil {
		return fmt.Errorf("tapecatalog: encode %s: %w", catalog.VolumeID, err)
	}
	data = append(data, '\n')
	if _, err := os.Lstat(siblingPath); err == nil {
		return fmt.Errorf(
			"tapecatalog: save %s: catalog already exists at %q; cannot also save at %q",
			catalog.VolumeID,
			siblingPath,
			path,
		)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(
			"tapecatalog: save %s: inspect sibling %q: %w",
			catalog.VolumeID,
			siblingPath,
			err,
		)
	}
	if err := os.MkdirAll(dir, 0o775); err != nil {
		return fmt.Errorf("tapecatalog: create directory: %w", err)
	}
	if err := maybe.WriteFile(path, data, 0o664); err != nil {
		return fmt.Errorf("tapecatalog: write %s: %w", catalog.VolumeID, err)
	}
	return nil
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
		if entry.IsDir() ||
			filepath.Ext(entry.Name()) != paths.VolumeCatalogExtension {
			continue
		}
		id, err := volume.ParseID(
			strings.TrimSuffix(entry.Name(), paths.VolumeCatalogExtension),
		)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

// Detach atomically moves a volume catalog from active to detached.
func Detach(stateRoot string, id volume.ID) error {
	return move(
		id,
		"detach",
		"active",
		"detached",
		paths.ActiveVolumeCatalog(stateRoot, string(id)),
		paths.DetachedVolumeCatalog(stateRoot, string(id)),
		paths.DetachedVolumeCatalogDir(stateRoot),
	)
}

// Attach atomically moves a volume catalog from detached to active.
func Attach(stateRoot string, id volume.ID) error {
	return move(
		id,
		"attach",
		"detached",
		"active",
		paths.DetachedVolumeCatalog(stateRoot, string(id)),
		paths.ActiveVolumeCatalog(stateRoot, string(id)),
		paths.ActiveVolumeCatalogDir(stateRoot),
	)
}

func move(
	id volume.ID,
	action, sourceSet, destinationSet, source, destination, destinationDir string,
) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()

	if err := validateVolumeID(id); err != nil {
		return err
	}
	if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(
			"tapecatalog: %s %s: %s catalog does not exist",
			action,
			id,
			sourceSet,
		)
	} else if err != nil {
		return fmt.Errorf("tapecatalog: %s %s: inspect source: %w", action, id, err)
	}
	if err := os.MkdirAll(destinationDir, 0o775); err != nil {
		return fmt.Errorf(
			"tapecatalog: %s %s: create destination directory: %w",
			action,
			id,
			err,
		)
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf(
			"tapecatalog: %s %s: %s catalog already exists",
			action,
			id,
			destinationSet,
		)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(
			"tapecatalog: %s %s: inspect destination: %w",
			action,
			id,
			err,
		)
	}
	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("tapecatalog: %s %s: rename: %w", action, id, err)
	}
	return nil
}

// Purge retires a volume by deleting its catalog from either set. Purging an
// already-absent volume succeeds.
func Purge(stateRoot string, id volume.ID) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()

	if err := validateVolumeID(id); err != nil {
		return err
	}
	var purgeErrors []error
	for _, path := range []string{
		paths.ActiveVolumeCatalog(stateRoot, string(id)),
		paths.DetachedVolumeCatalog(stateRoot, string(id)),
	} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			purgeErrors = append(
				purgeErrors,
				fmt.Errorf("tapecatalog: purge %s: %w", id, err),
			)
		}
	}
	return errors.Join(purgeErrors...)
}

func validateCatalog(catalog Catalog) error {
	if err := validateVolumeID(catalog.VolumeID); err != nil {
		return err
	}
	if _, err := tape.ParseID(string(catalog.TapeID)); err != nil {
		return fmt.Errorf("tapecatalog: %w", err)
	}
	if catalog.CapacityBytes < 0 {
		return errors.New("tapecatalog: capacityBytes must not be negative")
	}
	if catalog.FreeBytes < 0 {
		return errors.New("tapecatalog: freeBytes must not be negative")
	}
	if catalog.FreeBytes > catalog.CapacityBytes {
		return errors.New("tapecatalog: freeBytes must not exceed capacityBytes")
	}
	if catalog.CreatedAt.IsZero() {
		return errors.New("tapecatalog: createdAt is required")
	}
	if catalog.ObservedAt.IsZero() {
		return errors.New("tapecatalog: observedAt is required")
	}
	seen := make(map[snapshotKey]struct{}, len(catalog.Snapshots))
	for _, snapshot := range catalog.Snapshots {
		if err := validateSnapshot(snapshot); err != nil {
			return err
		}
		key := snapshotKey{
			metadataRef: snapshot.MetadataRef,
			generation:  snapshot.Generation,
		}
		if _, ok := seen[key]; ok {
			return fmt.Errorf(
				"tapecatalog: duplicate snapshot pair (%q, %d)",
				snapshot.MetadataRef,
				snapshot.Generation,
			)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateVolumeID(id volume.ID) error {
	if _, err := volume.ParseID(string(id)); err != nil {
		return fmt.Errorf("tapecatalog: %w", err)
	}
	return nil
}

func validateSnapshot(snapshot Snapshot) error {
	if snapshot.MetadataRef == "" {
		return errors.New("tapecatalog: snapshot metadataRef is required")
	}
	if snapshot.Generation < 1 {
		return fmt.Errorf(
			"tapecatalog: snapshot %q generation must be at least 1",
			snapshot.MetadataRef,
		)
	}
	if snapshot.Bytes < 0 {
		return fmt.Errorf(
			"tapecatalog: snapshot (%q, %d) bytes must not be negative",
			snapshot.MetadataRef,
			snapshot.Generation,
		)
	}
	if snapshot.PayloadFingerprint == "" {
		return fmt.Errorf(
			"tapecatalog: snapshot (%q, %d) payloadFingerprint is required",
			snapshot.MetadataRef,
			snapshot.Generation,
		)
	}
	if snapshot.CapturedAt.IsZero() {
		return fmt.Errorf(
			"tapecatalog: snapshot (%q, %d) capturedAt is required",
			snapshot.MetadataRef,
			snapshot.Generation,
		)
	}
	return nil
}

type snapshotKey struct {
	metadataRef string
	generation  int
}

func toWire(catalog Catalog) catalogWire {
	lastVerifiedAt := ""
	if !catalog.LastVerifiedAt.IsZero() {
		lastVerifiedAt = catalog.LastVerifiedAt.UTC().Format(time.RFC3339)
	}
	wire := catalogWire{
		SchemaVersion:  schemaVersion,
		VolumeID:       string(catalog.VolumeID),
		TapeID:         string(catalog.TapeID),
		CreatedAt:      catalog.CreatedAt.UTC().Format(time.RFC3339),
		ObservedAt:     catalog.ObservedAt.UTC().Format(time.RFC3339),
		CapacityBytes:  catalog.CapacityBytes,
		FreeBytes:      catalog.FreeBytes,
		LastVerifiedAt: lastVerifiedAt,
		Snapshots:      make([]snapshotWire, 0, len(catalog.Snapshots)),
	}
	for _, snapshot := range catalog.Snapshots {
		wire.Snapshots = append(wire.Snapshots, snapshotWire{
			MetadataRef:        snapshot.MetadataRef,
			Generation:         snapshot.Generation,
			Bytes:              snapshot.Bytes,
			PayloadFingerprint: snapshot.PayloadFingerprint,
			CapturedAt:         snapshot.CapturedAt.UTC().Format(time.RFC3339),
			Incomplete:         snapshot.Incomplete,
		})
	}
	sort.Slice(wire.Snapshots, func(i, j int) bool {
		if wire.Snapshots[i].MetadataRef != wire.Snapshots[j].MetadataRef {
			return wire.Snapshots[i].MetadataRef < wire.Snapshots[j].MetadataRef
		}
		return wire.Snapshots[i].Generation < wire.Snapshots[j].Generation
	})
	return wire
}

func fromWire(wire catalogWire) (Catalog, error) {
	createdAt, err := parseTime("createdAt", wire.CreatedAt)
	if err != nil {
		return Catalog{}, err
	}
	observedAt, err := parseTime("observedAt", wire.ObservedAt)
	if err != nil {
		return Catalog{}, err
	}
	var lastVerifiedAt time.Time
	if wire.LastVerifiedAt != "" {
		lastVerifiedAt, err = parseTime("lastVerifiedAt", wire.LastVerifiedAt)
		if err != nil {
			return Catalog{}, err
		}
	}
	catalog := Catalog{
		VolumeID:       volume.ID(wire.VolumeID),
		TapeID:         tape.ID(wire.TapeID),
		CreatedAt:      createdAt,
		ObservedAt:     observedAt,
		CapacityBytes:  wire.CapacityBytes,
		FreeBytes:      wire.FreeBytes,
		LastVerifiedAt: lastVerifiedAt,
		Snapshots:      make([]Snapshot, 0, len(wire.Snapshots)),
	}
	for _, snapshotWire := range wire.Snapshots {
		capturedAt, err := parseTime("snapshot capturedAt", snapshotWire.CapturedAt)
		if err != nil {
			return Catalog{}, err
		}
		catalog.Snapshots = append(catalog.Snapshots, Snapshot{
			MetadataRef:        snapshotWire.MetadataRef,
			Generation:         snapshotWire.Generation,
			Bytes:              snapshotWire.Bytes,
			PayloadFingerprint: snapshotWire.PayloadFingerprint,
			CapturedAt:         capturedAt,
			Incomplete:         snapshotWire.Incomplete,
		})
	}
	sort.Slice(catalog.Snapshots, func(i, j int) bool {
		if catalog.Snapshots[i].MetadataRef != catalog.Snapshots[j].MetadataRef {
			return catalog.Snapshots[i].MetadataRef < catalog.Snapshots[j].MetadataRef
		}
		return catalog.Snapshots[i].Generation < catalog.Snapshots[j].Generation
	})
	return catalog, nil
}

func parseTime(field, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("tapecatalog: parse %s: %w", field, err)
	}
	return parsed, nil
}

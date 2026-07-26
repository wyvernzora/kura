// Package tapecatalog owns the observed per-cartridge catalog at
// <state root>/tapes/<tapeID>.json. Catalogs are rebuildable caches of what
// an agent last observed on tape; the tape remains authoritative.
package tapecatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/renameio/v2/maybe"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/paths"
)

const schemaVersion = 1

// TapeID is the eight-character LTO Ultrium barcode printed on a cartridge.
type TapeID string

// MediaGeneration reports the cartridge generation encoded in the barcode's
// media identifier.
//
// Like snapshot directories and obsolete ratios, generation is derived rather
// than stored. Capacity arithmetic uses filesystem-observed capacity and free
// space rather than values inferred from the generation.
func (id TapeID) MediaGeneration() string {
	if validateTapeID(id) != nil {
		return ""
	}
	return mediaGenerationForIdentifier(string(id)[6:])
}

// Catalog records what the agent last observed on one physical cartridge.
type Catalog struct {
	TapeID         TapeID
	FormatID       string
	CreatedAt      time.Time
	ObservedAt     time.Time
	CapacityBytes  int64
	FreeBytes      int64
	LastVerifiedAt time.Time
	// Snapshots may be empty: a blank catalog is a registered cartridge with
	// full free space, and the catalog itself is the tape registry.
	Snapshots []Snapshot
}

// Snapshot identifies one series generation observed on a tape.
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
	TapeID         string         `json:"tapeID"`
	FormatID       string         `json:"formatID"`
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

// Load reads and validates one catalog. A missing catalog wraps os.ErrNotExist.
func Load(stateRoot string, id TapeID) (Catalog, error) {
	if err := validateTapeID(id); err != nil {
		return Catalog{}, err
	}
	data, err := os.ReadFile(paths.TapeCatalog(stateRoot, string(id)))
	if err != nil {
		return Catalog{}, fmt.Errorf("tapecatalog: read %s: %w", id, err)
	}
	var wire catalogWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return Catalog{}, fmt.Errorf("tapecatalog: decode %s: %w", id, err)
	}
	if wire.SchemaVersion != schemaVersion {
		return Catalog{}, fmt.Errorf("tapecatalog: unsupported schemaVersion %d", wire.SchemaVersion)
	}
	catalog, err := fromWire(wire)
	if err != nil {
		return Catalog{}, err
	}
	if err := validateCatalog(catalog); err != nil {
		return Catalog{}, err
	}
	if catalog.TapeID != id {
		return Catalog{}, fmt.Errorf(
			"tapecatalog: tapeID mismatch: filename is %q, file contains %q",
			id,
			catalog.TapeID,
		)
	}
	return catalog, nil
}

// Save validates and atomically rewrites one catalog.
//
// There is deliberately no compare-and-swap: the server is the sole writer,
// at most one backup plan is active, and this file is a rebuildable cache.
func Save(stateRoot string, catalog Catalog) error {
	if err := validateCatalog(catalog); err != nil {
		return err
	}
	data, err := json.MarshalIndent(toWire(catalog), "", "  ")
	if err != nil {
		return fmt.Errorf("tapecatalog: encode %s: %w", catalog.TapeID, err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(paths.TapeCatalogDir(stateRoot), 0o775); err != nil {
		return fmt.Errorf("tapecatalog: create directory: %w", err)
	}
	if err := maybe.WriteFile(
		paths.TapeCatalog(stateRoot, string(catalog.TapeID)),
		data,
		0o664,
	); err != nil {
		return fmt.Errorf("tapecatalog: write %s: %w", catalog.TapeID, err)
	}
	return nil
}

// List returns the sorted IDs of registered cartridges.
func List(stateRoot string) ([]TapeID, error) {
	entries, err := os.ReadDir(paths.TapeCatalogDir(stateRoot))
	if errors.Is(err, os.ErrNotExist) {
		return []TapeID{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tapecatalog: list: %w", err)
	}
	ids := make([]TapeID, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != paths.TapeCatalogExtension {
			continue
		}
		id := TapeID(strings.TrimSuffix(entry.Name(), paths.TapeCatalogExtension))
		if validateTapeID(id) != nil {
			continue
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

// Delete retires a cartridge by removing its catalog from the registry.
// There is deliberately no lifecycle field: an absent file means the tape
// is no longer registered. Deleting a catalog that is already absent succeeds.
func Delete(stateRoot string, id TapeID) error {
	if err := validateTapeID(id); err != nil {
		return err
	}
	err := os.Remove(paths.TapeCatalog(stateRoot, string(id)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("tapecatalog: delete %s: %w", id, err)
	}
	return nil
}

func validateTapeID(id TapeID) error {
	value := string(id)
	if value == "" {
		return errors.New("tapecatalog: tapeID is required")
	}
	if len(value) != 8 {
		return fmt.Errorf("tapecatalog: tapeID %q must be exactly 8 characters", value)
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' {
			return fmt.Errorf("tapecatalog: tapeID %q must use uppercase letters", value)
		}
	}
	for _, char := range value[:6] {
		if char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		return fmt.Errorf(
			"tapecatalog: tapeID %q volume serial must contain only A-Z and 0-9",
			value,
		)
	}
	mediaIdentifier := value[6:]
	if mediaIdentifier == "CU" {
		return fmt.Errorf("tapecatalog: tapeID %q identifies a cleaning cartridge", value)
	}
	if isWORMMediaIdentifier(mediaIdentifier) {
		return fmt.Errorf("tapecatalog: tapeID %q identifies WORM media", value)
	}
	if mediaGenerationForIdentifier(mediaIdentifier) == "" {
		return fmt.Errorf(
			"tapecatalog: tapeID %q has unsupported media identifier %q",
			value,
			mediaIdentifier,
		)
	}
	return nil
}

func isWORMMediaIdentifier(identifier string) bool {
	return len(identifier) == 2 &&
		identifier[0] == 'L' &&
		identifier[1] >= 'T' &&
		identifier[1] <= 'Z'
}

// Generation characters continue into letters (A = 10), so future
// generations require an explicit table entry rather than arithmetic.
var mediaGenerationByIdentifier = map[string]string{
	"L1": "LTO-1",
	"L2": "LTO-2",
	"L3": "LTO-3",
	"L4": "LTO-4",
	"L5": "LTO-5",
	"L6": "LTO-6",
	"L7": "LTO-7",
	"L8": "LTO-8",
	"L9": "LTO-9",
	"LA": "LTO-10",
	"M8": "LTO-7 Type M",
}

func mediaGenerationForIdentifier(identifier string) string {
	return mediaGenerationByIdentifier[identifier]
}

func validateCatalog(catalog Catalog) error {
	if err := validateTapeID(catalog.TapeID); err != nil {
		return err
	}
	if catalog.FormatID == "" {
		return errors.New("tapecatalog: formatID is required")
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
		key := snapshotKey{metadataRef: snapshot.MetadataRef, generation: snapshot.Generation}
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
		TapeID:         string(catalog.TapeID),
		FormatID:       catalog.FormatID,
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
		TapeID:         TapeID(wire.TapeID),
		FormatID:       wire.FormatID,
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
	return catalog, nil
}

func parseTime(field, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("tapecatalog: parse %s: %w", field, err)
	}
	return parsed, nil
}

// Package tapemanifest owns the permanent per-snapshot manifest and completion
// marker stored on an LTFS volume.
package tapemanifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/renameio/v2/maybe"
	"github.com/wyvernzora/kura/services/tape-backup/internal/snapshotname"
	"golang.org/x/text/unicode/norm"
)

const schemaVersion = 1

var (
	// ErrIncomplete identifies an interrupted snapshot without a completion
	// marker.
	ErrIncomplete = errors.New("tapemanifest: snapshot is incomplete")
)

// File records one archived file.
type File struct {
	Path    string
	Size    int64
	ModTime time.Time
	Hash    string
}

// Writer records the build and host that wrote a snapshot.
type Writer struct {
	Version string
	Host    string
}

// Manifest records one archived series generation.
type Manifest struct {
	MetadataRef string
	Title       string
	Generation  int
	CapturedAt  time.Time
	WrittenBy   Writer
	TotalBytes  int64
	Files       []File
}

type manifestWire struct {
	SchemaVersion int        `json:"schemaVersion"`
	MetadataRef   string     `json:"metadataRef"`
	Title         string     `json:"title"`
	Generation    int        `json:"generation"`
	CapturedAt    string     `json:"capturedAt"`
	WrittenBy     writerWire `json:"writtenBy"`
	TotalBytes    int64      `json:"totalBytes"`
	Files         []fileWire `json:"files"`
}

type writerWire struct {
	Version string `json:"version"`
	Host    string `json:"host"`
}

type fileWire struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	MTime string `json:"mtime"`
	Hash  string `json:"hash"`
}

type completeWire struct {
	SchemaVersion int    `json:"schemaVersion"`
	ManifestHash  string `json:"manifestHash"`
	CompletedAt   string `json:"completedAt"`
}

// Write validates and writes manifest.json without committing the snapshot.
func Write(snapshotDir string, manifest Manifest) error {
	if err := validateManifest(snapshotDir, manifest); err != nil {
		return err
	}
	data, err := json.MarshalIndent(toWire(manifest), "", "  ")
	if err != nil {
		return fmt.Errorf("tapemanifest: encode manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(snapshotDir, 0o775); err != nil {
		return fmt.Errorf("tapemanifest: create snapshot directory: %w", err)
	}
	if err := os.Remove(completeFile(snapshotDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("tapemanifest: remove completion marker: %w", err)
	}
	if err := maybe.WriteFile(manifestFile(snapshotDir), data, 0o664); err != nil {
		return fmt.Errorf("tapemanifest: write manifest: %w", err)
	}
	return nil
}

// Commit hashes manifest.json as stored and writes complete.json last.
func Commit(snapshotDir string) error {
	manifestData, err := os.ReadFile(manifestFile(snapshotDir))
	if err != nil {
		return fmt.Errorf("tapemanifest: read manifest for commit: %w", err)
	}
	if _, err := decodeManifest(snapshotDir, manifestData); err != nil {
		return err
	}
	sum := sha256.Sum256(manifestData)
	complete := completeWire{
		SchemaVersion: schemaVersion,
		ManifestHash:  "sha256:" + hex.EncodeToString(sum[:]),
		CompletedAt:   time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(complete, "", "  ")
	if err != nil {
		return fmt.Errorf("tapemanifest: encode complete: %w", err)
	}
	data = append(data, '\n')
	if err := maybe.WriteFile(completeFile(snapshotDir), data, 0o664); err != nil {
		return fmt.Errorf("tapemanifest: write complete: %w", err)
	}
	return nil
}

// Read returns a committed, checksum-verified manifest.
func Read(snapshotDir string) (Manifest, error) {
	completeData, err := os.ReadFile(completeFile(snapshotDir))
	if errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Stat(snapshotDir); statErr != nil {
			return Manifest{}, fmt.Errorf("tapemanifest: read snapshot directory: %w", statErr)
		}
		if _, statErr := os.Stat(manifestFile(snapshotDir)); statErr != nil {
			return Manifest{}, fmt.Errorf("tapemanifest: read manifest: %w", statErr)
		}
		return Manifest{}, ErrIncomplete
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("tapemanifest: read complete: %w", err)
	}
	complete, err := decodeComplete(completeData)
	if err != nil {
		return Manifest{}, err
	}

	manifestData, err := os.ReadFile(manifestFile(snapshotDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Manifest{}, errors.New("tapemanifest: committed manifest is missing")
		}
		return Manifest{}, fmt.Errorf("tapemanifest: read committed manifest: %w", err)
	}
	sum := sha256.Sum256(manifestData)
	actual := "sha256:" + hex.EncodeToString(sum[:])
	if complete.ManifestHash != actual {
		return Manifest{}, fmt.Errorf(
			"tapemanifest: manifest checksum mismatch: complete has %q, manifest is %q",
			complete.ManifestHash,
			actual,
		)
	}
	return decodeManifest(snapshotDir, manifestData)
}

// ReadManifest reads and validates manifest.json without requiring a
// completion marker.
func ReadManifest(snapshotDir string) (Manifest, error) {
	data, err := os.ReadFile(manifestFile(snapshotDir))
	if err != nil {
		return Manifest{}, fmt.Errorf("tapemanifest: read manifest: %w", err)
	}
	return decodeManifest(snapshotDir, data)
}

func decodeManifest(snapshotDir string, data []byte) (Manifest, error) {
	if !utf8.Valid(data) {
		return Manifest{}, errors.New("tapemanifest: manifest must be valid UTF-8")
	}
	var wire manifestWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return Manifest{}, fmt.Errorf("tapemanifest: decode manifest: %w", err)
	}
	if wire.SchemaVersion != schemaVersion {
		return Manifest{}, fmt.Errorf(
			"tapemanifest: unsupported manifest schemaVersion %d",
			wire.SchemaVersion,
		)
	}
	manifest, err := fromWire(wire)
	if err != nil {
		return Manifest{}, err
	}
	if err := validateManifest(snapshotDir, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func decodeComplete(data []byte) (completeWire, error) {
	if !utf8.Valid(data) {
		return completeWire{}, errors.New("tapemanifest: complete must be valid UTF-8")
	}
	var wire completeWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return completeWire{}, fmt.Errorf("tapemanifest: decode complete: %w", err)
	}
	if wire.SchemaVersion != schemaVersion {
		return completeWire{}, fmt.Errorf(
			"tapemanifest: unsupported complete schemaVersion %d",
			wire.SchemaVersion,
		)
	}
	if err := validateHash("manifestHash", wire.ManifestHash); err != nil {
		return completeWire{}, err
	}
	completedAt, err := time.Parse(time.RFC3339, wire.CompletedAt)
	if err != nil {
		return completeWire{}, fmt.Errorf("tapemanifest: parse completedAt: %w", err)
	}
	if _, offset := completedAt.Zone(); offset != 0 {
		return completeWire{}, errors.New("tapemanifest: completedAt must be UTC")
	}
	if completedAt.Nanosecond() != 0 {
		return completeWire{}, errors.New(
			"tapemanifest: completedAt must be truncated to whole seconds",
		)
	}
	return wire, nil
}

func validateManifest(snapshotDir string, manifest Manifest) error {
	if err := validateSnapshotName(snapshotDir, manifest); err != nil {
		return err
	}
	if err := validateManifestMetadata(manifest); err != nil {
		return err
	}
	if len(manifest.Files) == 0 {
		return errors.New("tapemanifest: files must contain at least one file")
	}
	if err := validateFiles(manifest.Files); err != nil {
		return err
	}

	var totalBytes int64
	for _, file := range manifest.Files {
		if file.Size > math.MaxInt64-totalBytes {
			return errors.New("tapemanifest: totalBytes overflow")
		}
		totalBytes += file.Size
	}
	if manifest.TotalBytes != totalBytes {
		return fmt.Errorf(
			"tapemanifest: totalBytes is %d, want sum of file sizes %d",
			manifest.TotalBytes,
			totalBytes,
		)
	}
	return nil
}

func validateManifestMetadata(manifest Manifest) error {
	if manifest.Title == "" {
		return errors.New("tapemanifest: title is required")
	}
	if !utf8.ValidString(manifest.Title) {
		return errors.New("tapemanifest: title must be valid UTF-8")
	}
	if !norm.NFC.IsNormalString(manifest.Title) {
		return errors.New("tapemanifest: title must be NFC-normalized")
	}
	if manifest.CapturedAt.IsZero() {
		return errors.New("tapemanifest: capturedAt is required")
	}
	if _, offset := manifest.CapturedAt.Zone(); offset != 0 {
		return errors.New("tapemanifest: capturedAt must be UTC")
	}
	if manifest.CapturedAt.Nanosecond() != 0 {
		return errors.New("tapemanifest: capturedAt must be truncated to whole seconds")
	}
	if manifest.WrittenBy.Version == "" {
		return errors.New("tapemanifest: writtenBy.version is required")
	}
	if !utf8.ValidString(manifest.WrittenBy.Version) {
		return errors.New("tapemanifest: writtenBy.version must be valid UTF-8")
	}
	if manifest.WrittenBy.Host == "" {
		return errors.New("tapemanifest: writtenBy.host is required")
	}
	if !utf8.ValidString(manifest.WrittenBy.Host) {
		return errors.New("tapemanifest: writtenBy.host must be valid UTF-8")
	}
	return nil
}

func validateSnapshotName(snapshotDir string, manifest Manifest) error {
	expected, err := snapshotname.Format(manifest.MetadataRef, manifest.Generation)
	if err != nil {
		return fmt.Errorf("tapemanifest: %w", err)
	}
	actual := filepath.Base(filepath.Clean(snapshotDir))
	if actual != expected {
		return fmt.Errorf(
			"tapemanifest: snapshot directory is %q, want %q for (%q, %d)",
			actual,
			expected,
			manifest.MetadataRef,
			manifest.Generation,
		)
	}
	return nil
}

func validateFiles(files []File) error {
	var previous string
	for i, file := range files {
		if err := validatePath(file.Path); err != nil {
			return err
		}
		if i > 0 {
			switch {
			case file.Path == previous:
				return fmt.Errorf("tapemanifest: duplicate file path %q", file.Path)
			case file.Path < previous:
				return fmt.Errorf(
					"tapemanifest: file paths are not sorted: %q precedes %q",
					file.Path,
					previous,
				)
			}
		}
		if file.Size < 0 {
			return fmt.Errorf("tapemanifest: size for %q must not be negative", file.Path)
		}
		if file.ModTime.IsZero() {
			return fmt.Errorf("tapemanifest: mtime for %q is required", file.Path)
		}
		if _, offset := file.ModTime.Zone(); offset != 0 {
			return fmt.Errorf("tapemanifest: mtime for %q must be UTC", file.Path)
		}
		if file.ModTime.Nanosecond() != 0 {
			return fmt.Errorf(
				"tapemanifest: mtime for %q must be truncated to whole seconds",
				file.Path,
			)
		}
		if err := validateHash("hash for "+strconv.Quote(file.Path), file.Hash); err != nil {
			return err
		}
		previous = file.Path
	}
	return nil
}

func validatePath(filePath string) error {
	if filePath == "" {
		return errors.New("tapemanifest: file path is required")
	}
	if !utf8.ValidString(filePath) {
		return fmt.Errorf("tapemanifest: file path %q is not valid UTF-8", filePath)
	}
	if strings.ContainsAny(filePath, "\x00\n") {
		return fmt.Errorf(
			"tapemanifest: file path %q must not contain NUL or newline",
			filePath,
		)
	}
	if path.IsAbs(filePath) {
		return fmt.Errorf("tapemanifest: file path %q must be relative", filePath)
	}
	for part := range strings.SplitSeq(filePath, "/") {
		if part == ".." {
			return fmt.Errorf("tapemanifest: file path %q must not contain ..", filePath)
		}
	}
	if path.Clean(filePath) != filePath || filePath == "." {
		return fmt.Errorf("tapemanifest: file path %q is not canonical", filePath)
	}
	if !norm.NFC.IsNormalString(filePath) {
		return fmt.Errorf("tapemanifest: file path %q must be NFC-normalized", filePath)
	}
	return nil
}

func validateHash(field, value string) error {
	algorithm, digest, found := strings.Cut(value, ":")
	if !found {
		return fmt.Errorf("tapemanifest: %s must use algorithm:digest format", field)
	}
	if algorithm != "sha256" {
		return fmt.Errorf(
			"tapemanifest: unsupported hash algorithm %q for %s",
			algorithm,
			field,
		)
	}
	if len(digest) != sha256.Size*2 {
		return fmt.Errorf(
			"tapemanifest: %s sha256 digest must be 64 lowercase hexadecimal characters",
			field,
		)
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || hex.EncodeToString(decoded) != digest {
		return fmt.Errorf(
			"tapemanifest: %s sha256 digest must be 64 lowercase hexadecimal characters",
			field,
		)
	}
	return nil
}

func toWire(manifest Manifest) manifestWire {
	files := make([]fileWire, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		files = append(files, fileWire{
			Path:  file.Path,
			Size:  file.Size,
			MTime: file.ModTime.UTC().Format(time.RFC3339),
			Hash:  file.Hash,
		})
	}
	return manifestWire{
		SchemaVersion: schemaVersion,
		MetadataRef:   manifest.MetadataRef,
		Title:         manifest.Title,
		Generation:    manifest.Generation,
		CapturedAt:    manifest.CapturedAt.UTC().Format(time.RFC3339),
		WrittenBy: writerWire{
			Version: manifest.WrittenBy.Version,
			Host:    manifest.WrittenBy.Host,
		},
		TotalBytes: manifest.TotalBytes,
		Files:      files,
	}
}

func fromWire(wire manifestWire) (Manifest, error) {
	var capturedAt time.Time
	if wire.CapturedAt != "" {
		var err error
		capturedAt, err = time.Parse(time.RFC3339, wire.CapturedAt)
		if err != nil {
			return Manifest{}, fmt.Errorf("tapemanifest: parse capturedAt: %w", err)
		}
	}
	files := make([]File, 0, len(wire.Files))
	for _, file := range wire.Files {
		modTime, err := time.Parse(time.RFC3339, file.MTime)
		if err != nil {
			return Manifest{}, fmt.Errorf(
				"tapemanifest: parse mtime for %q: %w",
				file.Path,
				err,
			)
		}
		files = append(files, File{
			Path:    file.Path,
			Size:    file.Size,
			ModTime: modTime,
			Hash:    file.Hash,
		})
	}
	return Manifest{
		MetadataRef: wire.MetadataRef,
		Title:       wire.Title,
		Generation:  wire.Generation,
		CapturedAt:  capturedAt,
		WrittenBy: Writer{
			Version: wire.WrittenBy.Version,
			Host:    wire.WrittenBy.Host,
		},
		TotalBytes: wire.TotalBytes,
		Files:      files,
	}, nil
}

func manifestFile(snapshotDir string) string {
	return filepath.Join(snapshotDir, "manifest.json")
}

func completeFile(snapshotDir string) string {
	return filepath.Join(snapshotDir, "complete.json")
}

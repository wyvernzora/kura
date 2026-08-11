package tapemanifest_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapemanifest"
)

const snapshotName = "OR3GIYR2GM3TAMBXGA.g7"

func TestManifestRoundTrip(t *testing.T) {
	snapshotDir := filepath.Join(t.TempDir(), snapshotName)
	manifest := validManifest(t)

	if err := tapemanifest.Write(snapshotDir, manifest); err != nil {
		t.Fatalf("Write: %v", err)
	}

	uncommitted, err := tapemanifest.ReadManifest(snapshotDir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if !reflect.DeepEqual(uncommitted, manifest) {
		t.Fatalf("ReadManifest = %#v, want %#v", uncommitted, manifest)
	}
	if err := tapemanifest.Commit(snapshotDir); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := tapemanifest.Read(snapshotDir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got, manifest) {
		t.Fatalf("Read = %#v, want %#v", got, manifest)
	}

	assertWireContracts(t, snapshotDir)
}

func TestReadWithoutCompleteIsIncomplete(t *testing.T) {
	snapshotDir := filepath.Join(t.TempDir(), snapshotName)
	if err := tapemanifest.Write(snapshotDir, validManifest(t)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	_, err := tapemanifest.Read(snapshotDir)
	if !errors.Is(err, tapemanifest.ErrIncomplete) {
		t.Fatalf("Read error = %v, want ErrIncomplete", err)
	}
}

func TestWriteRemovesCompletionBeforeManifestRewrite(t *testing.T) {
	snapshotDir := filepath.Join(t.TempDir(), snapshotName)
	if err := tapemanifest.Write(snapshotDir, validManifest(t)); err != nil {
		t.Fatalf("Write initial: %v", err)
	}
	if err := tapemanifest.Commit(snapshotDir); err != nil {
		t.Fatalf("Commit initial: %v", err)
	}
	if err := os.Remove(filepath.Join(snapshotDir, "manifest.json")); err != nil {
		t.Fatalf("Remove manifest: %v", err)
	}
	if err := os.Mkdir(filepath.Join(snapshotDir, "manifest.json"), 0o755); err != nil {
		t.Fatalf("Mkdir manifest path: %v", err)
	}

	err := tapemanifest.Write(snapshotDir, validManifest(t))
	if !errors.Is(err, syscall.EEXIST) {
		t.Fatalf("Write error = %v, want EEXIST", err)
	}
	_, statErr := os.Stat(filepath.Join(snapshotDir, "complete.json"))
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("complete.json exists after failed rewrite: %v", statErr)
	}
}

func TestCommitValidatesManifestBytes(t *testing.T) {
	snapshotDir := filepath.Join(t.TempDir(), snapshotName)
	if err := tapemanifest.Write(snapshotDir, validManifest(t)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	wire := readManifestWire(t, snapshotDir)
	wire["rootPath"] = ""
	writeManifestWire(t, snapshotDir, wire)

	err := tapemanifest.Commit(snapshotDir)
	const want = "tapemanifest: rootPath is required"
	if err == nil || err.Error() != want {
		t.Fatalf("Commit error = %v, want %q", err, want)
	}
	_, statErr := os.Stat(filepath.Join(snapshotDir, "complete.json"))
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("complete.json exists after rejected Commit: %v", statErr)
	}
}

func TestReadMissingSnapshotOrManifestWrapsNotExist(t *testing.T) {
	t.Run("snapshot directory", func(t *testing.T) {
		snapshotDir := filepath.Join(t.TempDir(), "missing", snapshotName)
		_, err := tapemanifest.Read(snapshotDir)
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Read error = %v, want os.ErrNotExist", err)
		}
		if errors.Is(err, tapemanifest.ErrIncomplete) {
			t.Fatalf("Read error = %v, must not be ErrIncomplete", err)
		}
	})

	t.Run("manifest", func(t *testing.T) {
		snapshotDir := filepath.Join(t.TempDir(), snapshotName)
		if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		_, err := tapemanifest.Read(snapshotDir)
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Read error = %v, want os.ErrNotExist", err)
		}
		if errors.Is(err, tapemanifest.ErrIncomplete) {
			t.Fatalf("Read error = %v, must not be ErrIncomplete", err)
		}
	})
}

func TestReadMissingCommittedManifestIsCorruption(t *testing.T) {
	snapshotDir := committedSnapshot(t)
	if err := os.Remove(filepath.Join(snapshotDir, "manifest.json")); err != nil {
		t.Fatalf("Remove manifest: %v", err)
	}

	_, err := tapemanifest.Read(snapshotDir)
	const want = "tapemanifest: committed manifest is missing"
	if err == nil || err.Error() != want {
		t.Fatalf("Read error = %v, want %q", err, want)
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Read error = %v, must not be os.ErrNotExist", err)
	}
}

func TestReadRejectsManifestChecksumMismatchAsCorruption(t *testing.T) {
	snapshotDir := filepath.Join(t.TempDir(), snapshotName)
	if err := tapemanifest.Write(snapshotDir, validManifest(t)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	manifestPath := filepath.Join(snapshotDir, "manifest.json")
	original, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile manifest: %v", err)
	}
	if err := tapemanifest.Commit(snapshotDir); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := os.WriteFile(manifestPath, append(original, ' '), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	_, err = tapemanifest.Read(snapshotDir)
	originalSum := sha256.Sum256(original)
	changedSum := sha256.Sum256(append(original, ' '))
	want := fmt.Sprintf(
		`tapemanifest: manifest checksum mismatch: complete has %q, manifest is %q`,
		"sha256:"+hex.EncodeToString(originalSum[:]),
		"sha256:"+hex.EncodeToString(changedSum[:]),
	)
	if err == nil || err.Error() != want {
		t.Fatalf("Read error = %v, want %q", err, want)
	}
	if errors.Is(err, tapemanifest.ErrIncomplete) {
		t.Fatalf("Read checksum error = %v, must be distinct from ErrIncomplete", err)
	}
}

func TestCommitWritesCompleteLast(t *testing.T) {
	snapshotDir := filepath.Join(t.TempDir(), snapshotName)
	if err := os.MkdirAll(filepath.Join(snapshotDir, "manifest.json"), 0o755); err != nil {
		t.Fatalf("MkdirAll manifest path: %v", err)
	}

	err := tapemanifest.Commit(snapshotDir)
	if !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("Commit error = %v, want EISDIR", err)
	}
	_, statErr := os.Stat(filepath.Join(snapshotDir, "complete.json"))
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("complete.json exists after pre-commit failure: %v", statErr)
	}
}

func TestManifestTotalBytesMatchesFiles(t *testing.T) {
	const want = "tapemanifest: totalBytes is 16, want sum of file sizes 15"
	assertWriteAndReadRejection(
		t,
		func(manifest *tapemanifest.Manifest) { manifest.TotalBytes++ },
		func(wire map[string]any) { wire["totalBytes"] = float64(16) },
		want,
	)
}

func TestManifestRequiresFile(t *testing.T) {
	const want = "tapemanifest: files must contain at least one file"
	assertWriteAndReadRejection(
		t,
		func(manifest *tapemanifest.Manifest) {
			manifest.Files = nil
			manifest.TotalBytes = 0
		},
		func(wire map[string]any) {
			wire["files"] = []any{}
			wire["totalBytes"] = float64(0)
		},
		want,
	)
}

func TestManifestRejectsNegativeFileSize(t *testing.T) {
	const want = `tapemanifest: size for "Season 1/Show - S01E01.mkv" must not be negative`
	assertWriteAndReadRejection(
		t,
		func(manifest *tapemanifest.Manifest) {
			manifest.Files[1].Size = -1
			manifest.TotalBytes = 3
		},
		func(wire map[string]any) {
			wireFiles(wire)[1]["size"] = float64(-1)
			wire["totalBytes"] = float64(3)
		},
		want,
	)
}

func TestManifestSchemaVersion(t *testing.T) {
	snapshotDir := filepath.Join(t.TempDir(), snapshotName)
	if err := tapemanifest.Write(snapshotDir, validManifest(t)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	wire := readManifestWire(t, snapshotDir)
	wire["schemaVersion"] = float64(2)
	writeManifestWire(t, snapshotDir, wire)

	const want = "tapemanifest: unsupported manifest schemaVersion 2"
	err := tapemanifest.Commit(snapshotDir)
	if err == nil || err.Error() != want {
		t.Fatalf("Commit error = %v, want %q", err, want)
	}
	writeCompletionForManifest(t, snapshotDir, 1, "2026-07-26T19:41:52Z")
	_, err = tapemanifest.Read(snapshotDir)
	if err == nil || err.Error() != want {
		t.Fatalf("Read error = %v, want %q", err, want)
	}
}

func TestCompleteSchemaVersion(t *testing.T) {
	snapshotDir := committedSnapshot(t)
	wire := readCompleteWire(t, snapshotDir)
	wire["schemaVersion"] = float64(2)
	writeCompleteWire(t, snapshotDir, wire)

	_, err := tapemanifest.Read(snapshotDir)
	const want = "tapemanifest: unsupported complete schemaVersion 2"
	if err == nil || err.Error() != want {
		t.Fatalf("Read error = %v, want %q", err, want)
	}
}

func TestManifestFileHashValidation(t *testing.T) {
	const want = `tapemanifest: hash for ".kura/series.json" sha256 digest ` +
		`must be 64 lowercase hexadecimal characters`
	assertWriteAndReadRejection(
		t,
		func(manifest *tapemanifest.Manifest) {
			manifest.Files[0].Hash = "sha256:short"
		},
		func(wire map[string]any) {
			wireFiles(wire)[0]["hash"] = "sha256:short"
		},
		want,
	)
}

func TestManifestFileHashRequiresFullSHA256Length(t *testing.T) {
	const want = `tapemanifest: hash for ".kura/series.json" sha256 digest ` +
		`must be 64 lowercase hexadecimal characters`
	shortHash := "sha256:" + strings.Repeat("a", 40)
	assertWriteAndReadRejection(
		t,
		func(manifest *tapemanifest.Manifest) {
			manifest.Files[0].Hash = shortHash
		},
		func(wire map[string]any) {
			wireFiles(wire)[0]["hash"] = shortHash
		},
		want,
	)
}

func TestManifestFileHashRequiresLowercase(t *testing.T) {
	const want = `tapemanifest: hash for ".kura/series.json" sha256 digest ` +
		`must be 64 lowercase hexadecimal characters`
	uppercaseHash := "sha256:" + strings.Repeat("A", 64)
	assertWriteAndReadRejection(
		t,
		func(manifest *tapemanifest.Manifest) {
			manifest.Files[0].Hash = uppercaseHash
		},
		func(wire map[string]any) {
			wireFiles(wire)[0]["hash"] = uppercaseHash
		},
		want,
	)
}

func TestManifestRejectsUnsupportedHashAlgorithm(t *testing.T) {
	const want = `tapemanifest: unsupported hash algorithm "sha512" for ` +
		`hash for ".kura/series.json"`
	assertWriteAndReadRejection(
		t,
		func(manifest *tapemanifest.Manifest) {
			manifest.Files[0].Hash = "sha512:" + strings.Repeat("a", 64)
		},
		func(wire map[string]any) {
			wireFiles(wire)[0]["hash"] = "sha512:" + strings.Repeat("a", 64)
		},
		want,
	)
}

func TestManifestMTimeValidation(t *testing.T) {
	cases := []struct {
		name      string
		modTime   time.Time
		wireMTime string
		want      string
	}{
		{
			name:      "whole seconds",
			modTime:   time.Date(2026, 7, 26, 18, 55, 2, 1, time.UTC),
			wireMTime: "2026-07-26T18:55:02.000000001Z",
			want: `tapemanifest: mtime for ".kura/series.json" ` +
				`must be truncated to whole seconds`,
		},
		{
			name:      "UTC",
			modTime:   time.Date(2026, 7, 26, 11, 55, 2, 0, time.FixedZone("PDT", -7*60*60)),
			wireMTime: "2026-07-26T11:55:02-07:00",
			want:      `tapemanifest: mtime for ".kura/series.json" must be UTC`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertWriteAndReadRejection(
				t,
				func(manifest *tapemanifest.Manifest) {
					manifest.Files[0].ModTime = tc.modTime
				},
				func(wire map[string]any) {
					wireFiles(wire)[0]["mtime"] = tc.wireMTime
				},
				tc.want,
			)
		})
	}
}

func TestManifestCapturedAtValidation(t *testing.T) {
	cases := []struct {
		name           string
		capturedAt     time.Time
		wireCapturedAt string
		want           string
	}{
		{
			name:           "required",
			capturedAt:     time.Time{},
			wireCapturedAt: "",
			want:           "tapemanifest: capturedAt is required",
		},
		{
			name: "UTC",
			capturedAt: time.Date(
				2026,
				7,
				26,
				19,
				4,
				11,
				0,
				time.FixedZone("PDT", -7*60*60),
			),
			wireCapturedAt: "2026-07-26T19:04:11-07:00",
			want:           "tapemanifest: capturedAt must be UTC",
		},
		{
			name: "whole seconds",
			capturedAt: time.Date(
				2026,
				7,
				26,
				19,
				4,
				11,
				1,
				time.UTC,
			),
			wireCapturedAt: "2026-07-26T19:04:11.000000001Z",
			want:           "tapemanifest: capturedAt must be truncated to whole seconds",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertWriteAndReadRejection(
				t,
				func(manifest *tapemanifest.Manifest) {
					manifest.CapturedAt = tc.capturedAt
				},
				func(wire map[string]any) {
					wire["capturedAt"] = tc.wireCapturedAt
				},
				tc.want,
			)
		})
	}
}

func TestCompleteTimeValidation(t *testing.T) {
	cases := []struct {
		name        string
		completedAt string
		want        string
	}{
		{
			name:        "UTC",
			completedAt: "2026-07-26T19:41:52-07:00",
			want:        "tapemanifest: completedAt must be UTC",
		},
		{
			name:        "whole seconds",
			completedAt: "2026-07-26T19:41:52.000000001Z",
			want:        "tapemanifest: completedAt must be truncated to whole seconds",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshotDir := committedSnapshot(t)
			wire := readCompleteWire(t, snapshotDir)
			wire["completedAt"] = tc.completedAt
			writeCompleteWire(t, snapshotDir, wire)

			_, err := tapemanifest.Read(snapshotDir)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Read error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestManifestTotalBytesOverflow(t *testing.T) {
	manifest := validManifest(t)
	manifest.Files = make([]tapemanifest.File, 4)
	for i := range manifest.Files {
		manifest.Files[i] = tapemanifest.File{
			Path:    fmt.Sprintf("file-%d", i),
			Size:    1<<62 + 1,
			ModTime: time.Date(2026, 7, 26, 18, 55, 2, 0, time.UTC),
			Hash:    "sha256:" + strings.Repeat("a", 64),
		}
	}

	err := tapemanifest.Write(filepath.Join(t.TempDir(), snapshotName), manifest)
	const want = "tapemanifest: totalBytes overflow"
	if err == nil || err.Error() != want {
		t.Fatalf("Write error = %v, want %q", err, want)
	}
}

func TestManifestMatchesSnapshotDirectory(t *testing.T) {
	manifest := validManifest(t)
	wrongDir := filepath.Join(t.TempDir(), "wrong.g7")
	want := `tapemanifest: snapshot directory is "wrong.g7", want ` +
		`"OR3GIYR2GM3TAMBXGA.g7" for ("tvdb:370070", 7)`
	if err := tapemanifest.Write(wrongDir, manifest); err == nil || err.Error() != want {
		t.Fatalf("Write error = %v, want %q", err, want)
	}

	root := t.TempDir()
	snapshotDir := filepath.Join(root, snapshotName)
	if err := tapemanifest.Write(snapshotDir, manifest); err != nil {
		t.Fatalf("Write valid: %v", err)
	}
	if err := tapemanifest.Commit(snapshotDir); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := os.Rename(snapshotDir, wrongDir); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	_, err := tapemanifest.Read(wrongDir)
	if err == nil || err.Error() != want {
		t.Fatalf("Read error = %v, want %q", err, want)
	}
}

func TestManifestSnapshotNameValidation(t *testing.T) {
	cases := []struct {
		name        string
		metadataRef string
		want        string
	}{
		{
			name:        "metadata ref valid UTF-8",
			metadataRef: string([]byte{0xff}),
			want:        "tapemanifest: metadataRef must be valid UTF-8",
		},
		{
			name:        "snapshot name length",
			metadataRef: strings.Repeat("a", 200),
			want:        "tapemanifest: snapshot name exceeds 255-byte limit",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := validManifest(t)
			manifest.MetadataRef = tc.metadataRef
			err := tapemanifest.Write(filepath.Join(t.TempDir(), snapshotName), manifest)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Write error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestManifestRootPathValidation(t *testing.T) {
	cases := []struct {
		name     string
		rootPath string
		want     string
	}{
		{
			name: "required",
			want: "tapemanifest: rootPath is required",
		},
		{
			name:     "valid UTF-8",
			rootPath: string([]byte{0xff}),
			want:     "tapemanifest: rootPath \"\\xff\" is not valid UTF-8",
		},
		{
			name:     "relative",
			rootPath: "/other-series",
			want:     `tapemanifest: rootPath "/other-series" must be relative`,
		},
		{
			name:     "traversal",
			rootPath: "../other-series",
			want:     `tapemanifest: rootPath "../other-series" must not contain ..`,
		},
		{
			name:     "canonical",
			rootPath: "Anime//Show",
			want:     `tapemanifest: rootPath "Anime//Show" is not canonical`,
		},
		{
			name:     "NFC",
			rootPath: "Cafe\u0301",
			want:     `tapemanifest: rootPath "Café" must be NFC-normalized`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := validManifest(t)
			manifest.RootPath = tc.rootPath
			err := tapemanifest.Write(filepath.Join(t.TempDir(), snapshotName), manifest)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Write error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestManifestDecodeRejectsRootPathTraversal(t *testing.T) {
	snapshotDir := filepath.Join(t.TempDir(), snapshotName)
	if err := tapemanifest.Write(snapshotDir, validManifest(t)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	wire := readManifestWire(t, snapshotDir)
	wire["rootPath"] = "../other-series"
	writeManifestWire(t, snapshotDir, wire)

	_, err := tapemanifest.ReadManifest(snapshotDir)
	const want = `tapemanifest: rootPath "../other-series" must not contain ..`
	if err == nil || err.Error() != want {
		t.Fatalf("ReadManifest error = %v, want %q", err, want)
	}
}

func TestManifestWrittenByValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*tapemanifest.Manifest)
		want   string
	}{
		{
			name: "version required",
			mutate: func(manifest *tapemanifest.Manifest) {
				manifest.WrittenBy.Version = ""
			},
			want: "tapemanifest: writtenBy.version is required",
		},
		{
			name: "host required",
			mutate: func(manifest *tapemanifest.Manifest) {
				manifest.WrittenBy.Host = ""
			},
			want: "tapemanifest: writtenBy.host is required",
		},
		{
			name: "version valid UTF-8",
			mutate: func(manifest *tapemanifest.Manifest) {
				manifest.WrittenBy.Version = "v0.\xff.0"
			},
			want: "tapemanifest: writtenBy.version must be valid UTF-8",
		},
		{
			name: "host valid UTF-8",
			mutate: func(manifest *tapemanifest.Manifest) {
				manifest.WrittenBy.Host = "tape-\xffvm"
			},
			want: "tapemanifest: writtenBy.host must be valid UTF-8",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := validManifest(t)
			tc.mutate(&manifest)
			err := tapemanifest.Write(filepath.Join(t.TempDir(), snapshotName), manifest)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Write error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestDecodeRejectsRawInvalidUTF8(t *testing.T) {
	t.Run("manifest writtenBy.version", func(t *testing.T) {
		snapshotDir := filepath.Join(t.TempDir(), snapshotName)
		if err := tapemanifest.Write(snapshotDir, validManifest(t)); err != nil {
			t.Fatalf("Write: %v", err)
		}
		manifestPath := filepath.Join(snapshotDir, "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("ReadFile manifest: %v", err)
		}
		data = bytes.Replace(data, []byte("v0.1.0"), []byte("v0.\xff.0"), 1)
		if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
			t.Fatalf("WriteFile manifest: %v", err)
		}

		_, err = tapemanifest.ReadManifest(snapshotDir)
		const want = "tapemanifest: manifest must be valid UTF-8"
		if err == nil || err.Error() != want {
			t.Fatalf("ReadManifest error = %v, want %q", err, want)
		}
	})

	t.Run("manifest writtenBy.host", func(t *testing.T) {
		snapshotDir := filepath.Join(t.TempDir(), snapshotName)
		if err := tapemanifest.Write(snapshotDir, validManifest(t)); err != nil {
			t.Fatalf("Write: %v", err)
		}
		manifestPath := filepath.Join(snapshotDir, "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("ReadFile manifest: %v", err)
		}
		data = bytes.Replace(data, []byte("tape-vm"), []byte("tape-\xffm"), 1)
		if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
			t.Fatalf("WriteFile manifest: %v", err)
		}

		_, err = tapemanifest.ReadManifest(snapshotDir)
		const want = "tapemanifest: manifest must be valid UTF-8"
		if err == nil || err.Error() != want {
			t.Fatalf("ReadManifest error = %v, want %q", err, want)
		}
	})

	t.Run("complete", func(t *testing.T) {
		snapshotDir := committedSnapshot(t)
		completePath := filepath.Join(snapshotDir, "complete.json")
		data, err := os.ReadFile(completePath)
		if err != nil {
			t.Fatalf("ReadFile complete: %v", err)
		}
		data = bytes.Replace(data, []byte("\n}"), []byte(",\n  \"probe\": \"\xff\"\n}"), 1)
		if err := os.WriteFile(completePath, data, 0o644); err != nil {
			t.Fatalf("WriteFile complete: %v", err)
		}

		_, err = tapemanifest.Read(snapshotDir)
		const want = "tapemanifest: complete must be valid UTF-8"
		if err == nil || err.Error() != want {
			t.Fatalf("Read error = %v, want %q", err, want)
		}
	})
}

func TestManifestRejectsInvalidUTF8FilePath(t *testing.T) {
	manifest := validManifest(t)
	manifest.Files[0].Path = ".bad\xff.mkv"

	err := tapemanifest.Write(filepath.Join(t.TempDir(), snapshotName), manifest)
	const want = "tapemanifest: file path \".bad\\xff.mkv\" is not valid UTF-8"
	if err == nil || err.Error() != want {
		t.Fatalf("Write error = %v, want %q", err, want)
	}
}

func TestManifestRejectsNonCanonicalFilePath(t *testing.T) {
	const want = `tapemanifest: file path ".kura//series.json" is not canonical`
	assertWriteAndReadRejection(
		t,
		func(manifest *tapemanifest.Manifest) {
			manifest.Files[0].Path = ".kura//series.json"
		},
		func(wire map[string]any) {
			wireFiles(wire)[0]["path"] = ".kura//series.json"
		},
		want,
	)
}

func TestManifestPathRules(t *testing.T) {
	cases := []struct {
		name           string
		mutateManifest func(*tapemanifest.Manifest)
		mutateWire     func(map[string]any)
		want           string
	}{
		{
			name: "absolute",
			mutateManifest: func(manifest *tapemanifest.Manifest) {
				manifest.Files[0].Path = "/absolute"
			},
			mutateWire: func(wire map[string]any) {
				wireFiles(wire)[0]["path"] = "/absolute"
			},
			want: `tapemanifest: file path "/absolute" must be relative`,
		},
		{
			name: "dot dot",
			mutateManifest: func(manifest *tapemanifest.Manifest) {
				manifest.Files[0].Path = "../escape"
			},
			mutateWire: func(wire map[string]any) {
				wireFiles(wire)[0]["path"] = "../escape"
			},
			want: `tapemanifest: file path "../escape" must not contain ..`,
		},
		{
			name: "NUL",
			mutateManifest: func(manifest *tapemanifest.Manifest) {
				manifest.Files[0].Path = "bad\x00path"
			},
			mutateWire: func(wire map[string]any) {
				wireFiles(wire)[0]["path"] = "bad\u0000path"
			},
			want: "tapemanifest: file path \"bad\\x00path\" must not contain NUL or newline",
		},
		{
			name: "newline",
			mutateManifest: func(manifest *tapemanifest.Manifest) {
				manifest.Files[0].Path = "bad\npath"
			},
			mutateWire: func(wire map[string]any) {
				wireFiles(wire)[0]["path"] = "bad\npath"
			},
			want: "tapemanifest: file path \"bad\\npath\" must not contain NUL or newline",
		},
		{
			name: "duplicate",
			mutateManifest: func(manifest *tapemanifest.Manifest) {
				manifest.Files[1].Path = manifest.Files[0].Path
			},
			mutateWire: func(wire map[string]any) {
				files := wireFiles(wire)
				files[1]["path"] = files[0]["path"]
			},
			want: `tapemanifest: duplicate file path ".kura/series.json"`,
		},
		{
			name: "unsorted",
			mutateManifest: func(manifest *tapemanifest.Manifest) {
				manifest.Files[0], manifest.Files[1] = manifest.Files[1], manifest.Files[0]
			},
			mutateWire: func(wire map[string]any) {
				files := wire["files"].([]any)
				files[0], files[1] = files[1], files[0]
			},
			want: `tapemanifest: file paths are not sorted: ` +
				`".kura/series.json" precedes "Season 1/Show - S01E01.mkv"`,
		},
		{
			name: "non NFC",
			mutateManifest: func(manifest *tapemanifest.Manifest) {
				manifest.Files[0].Path = "Cafe\u0301.ass"
			},
			mutateWire: func(wire map[string]any) {
				wireFiles(wire)[0]["path"] = "Cafe\u0301.ass"
			},
			want: "tapemanifest: file path \"Cafe\u0301.ass\" must be NFC-normalized",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertWriteAndReadRejection(
				t,
				func(manifest *tapemanifest.Manifest) {
					tc.mutateManifest(manifest)
				},
				tc.mutateWire,
				tc.want,
			)
		})
	}
}

func TestCompleteManifestHashValidation(t *testing.T) {
	snapshotDir := committedSnapshot(t)
	wire := readCompleteWire(t, snapshotDir)
	wire["manifestHash"] = "sha512:" + strings.Repeat("a", 64)
	writeCompleteWire(t, snapshotDir, wire)

	_, err := tapemanifest.Read(snapshotDir)
	const want = `tapemanifest: unsupported hash algorithm "sha512" for manifestHash`
	if err == nil || err.Error() != want {
		t.Fatalf("Read error = %v, want %q", err, want)
	}
}

func TestReadManifestIgnoresCompletionMarker(t *testing.T) {
	snapshotDir := filepath.Join(t.TempDir(), snapshotName)
	manifest := validManifest(t)
	if err := tapemanifest.Write(snapshotDir, manifest); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(snapshotDir, "complete.json"),
		[]byte("not-json"),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile complete: %v", err)
	}

	got, err := tapemanifest.ReadManifest(snapshotDir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if !reflect.DeepEqual(got, manifest) {
		t.Fatalf("ReadManifest = %#v, want %#v", got, manifest)
	}
}

func validManifest(t *testing.T) tapemanifest.Manifest {
	t.Helper()
	files := []tapemanifest.File{
		{
			Path:    ".kura/series.json",
			Size:    4,
			ModTime: time.Date(2026, 7, 26, 18, 55, 2, 0, time.UTC),
			Hash:    "sha256:" + strings.Repeat("a", 64),
		},
		{
			Path:    "Season 1/Show - S01E01.mkv",
			Size:    11,
			ModTime: time.Date(2026, 3, 14, 8, 21, 5, 0, time.UTC),
			Hash:    "sha256:" + strings.Repeat("b", 64),
		},
	}
	manifest := tapemanifest.Manifest{
		MetadataRef: "tvdb:370070",
		RootPath:    "Show",
		Generation:  7,
		CapturedAt: time.Date(
			2026,
			7,
			26,
			19,
			4,
			11,
			0,
			time.UTC,
		),
		WrittenBy: tapemanifest.Writer{
			Version: "v0.1.0",
			Host:    "tape-vm",
		},
		TotalBytes: 15,
		Files:      files,
	}
	return manifest
}

func assertWriteAndReadRejection(
	t *testing.T,
	mutateManifest func(*tapemanifest.Manifest),
	mutateWire func(map[string]any),
	want string,
) {
	t.Helper()
	t.Run("write", func(t *testing.T) {
		snapshotDir := filepath.Join(t.TempDir(), snapshotName)
		manifest := validManifest(t)
		mutateManifest(&manifest)
		err := tapemanifest.Write(snapshotDir, manifest)
		if err == nil || err.Error() != want {
			t.Fatalf("Write error = %v, want %q", err, want)
		}
		_, statErr := os.Stat(filepath.Join(snapshotDir, "manifest.json"))
		if !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("manifest exists after rejected Write: %v", statErr)
		}
	})

	t.Run("read", func(t *testing.T) {
		snapshotDir := filepath.Join(t.TempDir(), snapshotName)
		if err := tapemanifest.Write(snapshotDir, validManifest(t)); err != nil {
			t.Fatalf("Write valid: %v", err)
		}
		wire := readManifestWire(t, snapshotDir)
		mutateWire(wire)
		writeManifestWire(t, snapshotDir, wire)
		err := tapemanifest.Commit(snapshotDir)
		if err == nil || err.Error() != want {
			t.Fatalf("Commit error = %v, want %q", err, want)
		}
		writeCompletionForManifest(t, snapshotDir, 1, "2026-07-26T19:41:52Z")
		_, err = tapemanifest.Read(snapshotDir)
		if err == nil || err.Error() != want {
			t.Fatalf("Read error = %v, want %q", err, want)
		}
	})
}

func readManifestWire(t *testing.T, snapshotDir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(snapshotDir, "manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile manifest: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("Unmarshal manifest: %v", err)
	}
	return wire
}

func writeManifestWire(t *testing.T, snapshotDir string, wire map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent manifest: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(snapshotDir, "manifest.json"),
		append(data, '\n'),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
}

func committedSnapshot(t *testing.T) string {
	t.Helper()
	snapshotDir := filepath.Join(t.TempDir(), snapshotName)
	if err := tapemanifest.Write(snapshotDir, validManifest(t)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tapemanifest.Commit(snapshotDir); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return snapshotDir
}

func writeCompletionForManifest(
	t *testing.T,
	snapshotDir string,
	schemaVersion int,
	completedAt string,
) {
	t.Helper()
	manifestData, err := os.ReadFile(filepath.Join(snapshotDir, "manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile manifest: %v", err)
	}
	sum := sha256.Sum256(manifestData)
	writeCompleteWire(t, snapshotDir, map[string]any{
		"schemaVersion": float64(schemaVersion),
		"manifestHash":  "sha256:" + hex.EncodeToString(sum[:]),
		"completedAt":   completedAt,
	})
}

func readCompleteWire(t *testing.T, snapshotDir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(snapshotDir, "complete.json"))
	if err != nil {
		t.Fatalf("ReadFile complete: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("Unmarshal complete: %v", err)
	}
	return wire
}

func writeCompleteWire(t *testing.T, snapshotDir string, wire map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent complete: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(snapshotDir, "complete.json"),
		append(data, '\n'),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile complete: %v", err)
	}
}

func wireFiles(wire map[string]any) []map[string]any {
	raw := wire["files"].([]any)
	files := make([]map[string]any, 0, len(raw))
	for _, file := range raw {
		files = append(files, file.(map[string]any))
	}
	return files
}

func assertWireContracts(t *testing.T, snapshotDir string) {
	t.Helper()
	manifestData, err := os.ReadFile(filepath.Join(snapshotDir, "manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile manifest: %v", err)
	}
	var manifestWire map[string]any
	if err := json.Unmarshal(manifestData, &manifestWire); err != nil {
		t.Fatalf("Unmarshal manifest: %v", err)
	}
	if len(manifestWire) != 8 {
		t.Fatalf("manifest field count = %d, want 8: %s", len(manifestWire), manifestData)
	}
	if manifestWire["schemaVersion"] != float64(1) ||
		manifestWire["metadataRef"] != "tvdb:370070" ||
		manifestWire["rootPath"] != "Show" ||
		manifestWire["generation"] != float64(7) ||
		manifestWire["capturedAt"] != "2026-07-26T19:04:11Z" ||
		manifestWire["totalBytes"] != float64(15) {
		t.Fatalf("manifest wire = %#v, want documented fields", manifestWire)
	}
	writtenBy, ok := manifestWire["writtenBy"].(map[string]any)
	if !ok || len(writtenBy) != 2 ||
		writtenBy["version"] != "v0.1.0" ||
		writtenBy["host"] != "tape-vm" {
		t.Fatalf("manifest writtenBy = %#v, want documented fields", manifestWire["writtenBy"])
	}
	files := wireFiles(manifestWire)
	if len(files) != 2 ||
		files[0]["path"] != ".kura/series.json" ||
		files[0]["size"] != float64(4) ||
		files[0]["mtime"] != "2026-07-26T18:55:02Z" ||
		files[0]["hash"] != "sha256:"+strings.Repeat("a", 64) {
		t.Fatalf("manifest files = %#v, want documented fields", files)
	}

	completeData, err := os.ReadFile(filepath.Join(snapshotDir, "complete.json"))
	if err != nil {
		t.Fatalf("ReadFile complete: %v", err)
	}
	var completeWire map[string]any
	if err := json.Unmarshal(completeData, &completeWire); err != nil {
		t.Fatalf("Unmarshal complete: %v", err)
	}
	if len(completeWire) != 3 {
		t.Fatalf("complete field count = %d, want 3: %s", len(completeWire), completeData)
	}
	sum := sha256.Sum256(manifestData)
	if completeWire["schemaVersion"] != float64(1) ||
		completeWire["manifestHash"] != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("complete wire = %#v, want manifest checksum", completeWire)
	}
	completedAt, ok := completeWire["completedAt"].(string)
	if !ok {
		t.Fatalf("completedAt = %#v, want string", completeWire["completedAt"])
	}
	parsed, err := time.Parse(time.RFC3339, completedAt)
	if err != nil {
		t.Fatalf("Parse completedAt: %v", err)
	}
	if _, offset := parsed.Zone(); offset != 0 {
		t.Fatalf("completedAt offset = %d, want UTC", offset)
	}
}

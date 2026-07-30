package tapevolume_test

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

func TestVolumeRoundTrip(t *testing.T) {
	root := t.TempDir()
	volume := validVolume()

	if err := tapevolume.Write(root, volume); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := tapevolume.Read(root)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got, volume) {
		t.Fatalf("Read = %#v, want %#v", got, volume)
	}

	data, err := os.ReadFile(tapevolume.VolumeFile(root))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(wire) != 5 {
		t.Fatalf("volume field count = %d, want 5: %s", len(wire), data)
	}
	if wire["schemaVersion"] != float64(1) ||
		wire["volumeID"] != "01J8ZQ7W5TWHA6R6J8X4QZ9Y7V" ||
		wire["tapeID"] != "ABC123L6" ||
		wire["keyFingerprint"] != "630dcd2966c43366" ||
		wire["createdAt"] != "2026-07-21T12:00:00Z" {
		t.Fatalf("volume wire = %#v, want documented fields", wire)
	}
}

func TestVolumeUnsupportedSchemaVersion(t *testing.T) {
	root := t.TempDir()
	wire := validVolumeWire()
	wire["schemaVersion"] = 2
	writeVolumeWire(t, root, wire)

	_, err := tapevolume.Read(root)
	want := "tapevolume: unsupported schemaVersion 2"
	if err == nil || err.Error() != want {
		t.Fatalf("Read error = %v, want %q", err, want)
	}
}

func TestVolumeValidation(t *testing.T) {
	nonUTC := time.Date(2026, 7, 21, 12, 0, 0, 0, time.FixedZone("PDT", -7*60*60))
	cases := []struct {
		name       string
		mutate     func(*tapevolume.Volume)
		mutateWire func(map[string]any)
		want       string
	}{
		{
			name:       "key fingerprint required",
			mutate:     func(v *tapevolume.Volume) { v.KeyFingerprint = "" },
			mutateWire: func(w map[string]any) { w["keyFingerprint"] = "" },
			want:       "tapevolume: keyFingerprint must be 16 lowercase hexadecimal characters",
		},
		{
			name:       "key fingerprint lowercase hex",
			mutate:     func(v *tapevolume.Volume) { v.KeyFingerprint = "630DCD2966C43366" },
			mutateWire: func(w map[string]any) { w["keyFingerprint"] = "630DCD2966C43366" },
			want:       "tapevolume: keyFingerprint must be 16 lowercase hexadecimal characters",
		},
		{
			name:       "volume ID required",
			mutate:     func(v *tapevolume.Volume) { v.VolumeID = "" },
			mutateWire: func(w map[string]any) { w["volumeID"] = "" },
			want:       "tapevolume: volumeID is required",
		},
		{
			name: "volume ID canonical ULID",
			mutate: func(v *tapevolume.Volume) {
				v.VolumeID = "../01J8ZQ7W5TWHA6R6J8X4Q"
			},
			mutateWire: func(w map[string]any) {
				w["volumeID"] = "../01J8ZQ7W5TWHA6R6J8X4Q"
			},
			want: `tapevolume: volumeID "../01J8ZQ7W5TWHA6R6J8X4Q" must be a 26-character uppercase Crockford base32 ULID`,
		},
		{
			name:       "tape ID required",
			mutate:     func(v *tapevolume.Volume) { v.TapeID = "" },
			mutateWire: func(w map[string]any) { w["tapeID"] = "" },
			want:       "tapevolume: tapeID is required",
		},
		{
			name:       "tape ID valid",
			mutate:     func(v *tapevolume.Volume) { v.TapeID = "ABC123CU" },
			mutateWire: func(w map[string]any) { w["tapeID"] = "ABC123CU" },
			want:       `tapevolume: tapeID "ABC123CU" identifies a cleaning cartridge`,
		},
		{
			name:       "created at required",
			mutate:     func(v *tapevolume.Volume) { v.CreatedAt = time.Time{} },
			mutateWire: func(w map[string]any) { w["createdAt"] = "0001-01-01T00:00:00Z" },
			want:       "tapevolume: createdAt is required",
		},
		{
			name:   "created at UTC",
			mutate: func(v *tapevolume.Volume) { v.CreatedAt = nonUTC },
			mutateWire: func(w map[string]any) {
				w["createdAt"] = "2026-07-21T12:00:00-07:00"
			},
			want: "tapevolume: createdAt must be UTC",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("write", func(t *testing.T) {
				root := t.TempDir()
				volume := validVolume()
				tc.mutate(&volume)
				err := tapevolume.Write(root, volume)
				if err == nil || err.Error() != tc.want {
					t.Fatalf("Write error = %v, want %q", err, tc.want)
				}
				if _, statErr := os.Stat(tapevolume.VolumeFile(root)); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("volume file exists after rejected Write: %v", statErr)
				}
			})

			t.Run("read", func(t *testing.T) {
				root := t.TempDir()
				wire := validVolumeWire()
				tc.mutateWire(wire)
				writeVolumeWire(t, root, wire)
				_, err := tapevolume.Read(root)
				if err == nil || err.Error() != tc.want {
					t.Fatalf("Read error = %v, want %q", err, tc.want)
				}
			})
		})
	}
}

func TestVolumeCreatedAtMustBeRFC3339(t *testing.T) {
	root := t.TempDir()
	wire := validVolumeWire()
	wire["createdAt"] = "not-a-time"
	writeVolumeWire(t, root, wire)

	_, err := tapevolume.Read(root)
	want := `tapevolume: parse createdAt: parsing time "not-a-time" as "2006-01-02T15:04:05Z07:00": cannot parse "not-a-time" as "2006"`
	if err == nil || err.Error() != want {
		t.Fatalf("Read error = %v, want %q", err, want)
	}
}

func TestVolumeWriteOverwritesLeavingNoTempFiles(t *testing.T) {
	// True crash atomicity is a property of renameio and is not asserted here.
	root := t.TempDir()
	first := validVolume()
	if err := tapevolume.Write(root, first); err != nil {
		t.Fatalf("first Write: %v", err)
	}

	second := first
	second.VolumeID = "01J8ZQ7W5TWHA6R6J8X4QZ9Y7W"
	if err := tapevolume.Write(root, second); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	got, err := tapevolume.Read(root)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got, second) {
		t.Fatalf("Read = %#v, want %#v", got, second)
	}

	entries, err := os.ReadDir(tapevolume.ArchiveDir(root))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "volume.json" {
		t.Fatalf("archive entries = %v, want only volume.json", entryNames(entries))
	}
}

func TestVolumeReadMissing(t *testing.T) {
	_, err := tapevolume.Read(t.TempDir())
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Read error = %v, want os.ErrNotExist", err)
	}
}

func validVolume() tapevolume.Volume {
	return tapevolume.Volume{
		VolumeID:       volume.ID("01J8ZQ7W5TWHA6R6J8X4QZ9Y7V"),
		TapeID:         tape.ID("ABC123L6"),
		KeyFingerprint: "630dcd2966c43366",
		CreatedAt:      time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
	}
}

func validVolumeWire() map[string]any {
	return map[string]any{
		"schemaVersion":  1,
		"volumeID":       "01J8ZQ7W5TWHA6R6J8X4QZ9Y7V",
		"tapeID":         "ABC123L6",
		"keyFingerprint": "630dcd2966c43366",
		"createdAt":      "2026-07-21T12:00:00Z",
	}
}

func writeVolumeWire(t *testing.T, root string, wire map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	if err := os.MkdirAll(tapevolume.ArchiveDir(root), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(tapevolume.VolumeFile(root), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

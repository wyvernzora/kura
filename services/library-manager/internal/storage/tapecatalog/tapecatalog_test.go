package tapecatalog_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/tapecatalog"
)

func TestRoundTrip(t *testing.T) {
	root := t.TempDir()
	catalog := validCatalog()
	catalog.LastVerifiedAt = time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	catalog.Snapshots[0].Incomplete = true

	if err := tapecatalog.Save(root, catalog); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := tapecatalog.Load(root, catalog.TapeID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, catalog) {
		t.Fatalf("Load = %#v, want %#v", got, catalog)
	}

	data, err := os.ReadFile(paths.TapeCatalog(root, string(catalog.TapeID)))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(data, []byte(`"incomplete": true`)) {
		t.Fatalf("catalog does not contain incomplete marker:\n%s", data)
	}
	if bytes.Contains(data, []byte(`"mediaGeneration"`)) {
		t.Fatalf("catalog contains derived mediaGeneration:\n%s", data)
	}
}

func TestBlankRegisteredTapeRoundTrip(t *testing.T) {
	root := t.TempDir()
	catalog := validCatalog()
	catalog.FreeBytes = catalog.CapacityBytes
	catalog.LastVerifiedAt = time.Time{}
	catalog.Snapshots = []tapecatalog.Snapshot{}

	if err := tapecatalog.Save(root, catalog); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := tapecatalog.Load(root, catalog.TapeID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, catalog) {
		t.Fatalf("Load = %#v, want %#v", got, catalog)
	}

	data, err := os.ReadFile(paths.TapeCatalog(root, string(catalog.TapeID)))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(data, []byte(`"snapshots": []`)) {
		t.Fatalf("blank catalog snapshots are not an empty array:\n%s", data)
	}
	if bytes.Contains(data, []byte(`"lastVerifiedAt"`)) {
		t.Fatalf("blank catalog contains lastVerifiedAt:\n%s", data)
	}
}

func TestUnsupportedSchemaVersion(t *testing.T) {
	root := t.TempDir()
	wire := validWire()
	wire["schemaVersion"] = 2
	writeWire(t, root, "ABC123L6", wire)

	_, err := tapecatalog.Load(root, "ABC123L6")
	if err == nil || !strings.Contains(err.Error(), "unsupported schemaVersion 2") {
		t.Fatalf("Load error = %v, want unsupported schemaVersion 2", err)
	}
}

func TestTapeIDValidation(t *testing.T) {
	cases := []struct {
		name string
		id   tapecatalog.TapeID
		want string
	}{
		{name: "LTO-1 data", id: "ABC123L1"},
		{name: "LTO-2 data", id: "ABC123L2"},
		{name: "LTO-3 data", id: "ABC123L3"},
		{name: "LTO-4 data", id: "ABC123L4"},
		{name: "LTO-5 data", id: "ABC123L5"},
		{name: "LTO-6 data", id: "ABC123L6"},
		{name: "LTO-7 data", id: "ABC123L7"},
		{name: "LTO-8 data", id: "ABC123L8"},
		{name: "LTO-9 data", id: "ABC123L9"},
		{name: "LTO-10 data", id: "ABC123LA"},
		{name: "LTO-7 Type M", id: "ABC123M8"},
		{
			name: "cleaning cartridge rejected",
			id:   "ABC123CU",
			want: `tapecatalog: tapeID "ABC123CU" identifies a cleaning cartridge`,
		},
		{
			name: "WORM cartridge rejected",
			id:   "ABC123LW",
			want: `tapecatalog: tapeID "ABC123LW" identifies WORM media`,
		},
		{
			name: "unassigned LR rejected as unsupported",
			id:   "ABC123LR",
			want: `tapecatalog: tapeID "ABC123LR" has unsupported media identifier "LR"`,
		},
		{
			name: "unassigned LS rejected as unsupported",
			id:   "ABC123LS",
			want: `tapecatalog: tapeID "ABC123LS" has unsupported media identifier "LS"`,
		},
		{
			name: "unsupported media identifier rejected",
			id:   "ABC123ZZ",
			want: `tapecatalog: tapeID "ABC123ZZ" has unsupported media identifier "ZZ"`,
		},
		{
			name: "lowercase serial rejected",
			id:   "abc123L6",
			want: `tapecatalog: tapeID "abc123L6" must use uppercase letters`,
		},
		{
			name: "seven characters rejected",
			id:   "ABC12L6",
			want: `tapecatalog: tapeID "ABC12L6" must be exactly 8 characters`,
		},
		{
			name: "nine characters rejected",
			id:   "ABCD123L6",
			want: `tapecatalog: tapeID "ABCD123L6" must be exactly 8 characters`,
		},
		{
			name: "lowercase suffix rejected",
			id:   "ABC123l6",
			want: `tapecatalog: tapeID "ABC123l6" must use uppercase letters`,
		},
		{
			name: "non-alphanumeric serial rejected",
			id:   "ABC-23L6",
			want: `tapecatalog: tapeID "ABC-23L6" volume serial must contain only A-Z and 0-9`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			catalog := validCatalog()
			catalog.TapeID = tc.id
			err := tapecatalog.Save(t.TempDir(), catalog)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Save: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Save error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestTapeIDMediaGeneration(t *testing.T) {
	cases := []struct {
		name string
		id   tapecatalog.TapeID
		want string
	}{
		{name: "LTO-1", id: "ABC123L1", want: "LTO-1"},
		{name: "LTO-2", id: "ABC123L2", want: "LTO-2"},
		{name: "LTO-3", id: "ABC123L3", want: "LTO-3"},
		{name: "LTO-4", id: "ABC123L4", want: "LTO-4"},
		{name: "LTO-5", id: "ABC123L5", want: "LTO-5"},
		{name: "LTO-6", id: "ABC123L6", want: "LTO-6"},
		{name: "LTO-7", id: "ABC123L7", want: "LTO-7"},
		{name: "LTO-8", id: "ABC123L8", want: "LTO-8"},
		{name: "LTO-9", id: "ABC123L9", want: "LTO-9"},
		{name: "LTO-10", id: "ABC123LA", want: "LTO-10"},
		{name: "LTO-7 Type M", id: "ABC123M8", want: "LTO-7 Type M"},
		{name: "L0", id: "ABC123L0"},
		{name: "unsupported identifier", id: "ABC123ZZ"},
		{name: "cleaning cartridge", id: "ABC123CU"},
		{name: "WORM cartridge", id: "ABC123LW"},
		{name: "lowercase", id: "abc123l6"},
		{name: "invalid serial with allowlisted suffix", id: "ABC-23L6"},
		{name: "short", id: "ABC"},
		{name: "empty", id: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.id.MediaGeneration(); got != tc.want {
				t.Fatalf("MediaGeneration = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSaveValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*tapecatalog.Catalog)
		want   string
	}{
		{
			name:   "format ID required",
			mutate: func(c *tapecatalog.Catalog) { c.FormatID = "" },
			want:   "formatID",
		},
		{
			name:   "capacity bytes nonnegative",
			mutate: func(c *tapecatalog.Catalog) { c.CapacityBytes = -1 },
			want:   "capacityBytes must not be negative",
		},
		{
			name:   "free bytes nonnegative",
			mutate: func(c *tapecatalog.Catalog) { c.FreeBytes = -1 },
			want:   "freeBytes",
		},
		{
			name: "free bytes within capacity",
			mutate: func(c *tapecatalog.Catalog) {
				c.FreeBytes = c.CapacityBytes + 1
			},
			want: "freeBytes",
		},
		{
			name:   "created at required",
			mutate: func(c *tapecatalog.Catalog) { c.CreatedAt = time.Time{} },
			want:   "createdAt",
		},
		{
			name:   "observed at required",
			mutate: func(c *tapecatalog.Catalog) { c.ObservedAt = time.Time{} },
			want:   "observedAt",
		},
		{
			name: "snapshot metadata ref required",
			mutate: func(c *tapecatalog.Catalog) {
				c.Snapshots[0].MetadataRef = ""
			},
			want: "metadataRef",
		},
		{
			name: "snapshot generation positive",
			mutate: func(c *tapecatalog.Catalog) {
				c.Snapshots[0].Generation = 0
			},
			want: "generation",
		},
		{
			name: "snapshot bytes nonnegative",
			mutate: func(c *tapecatalog.Catalog) {
				c.Snapshots[0].Bytes = -1
			},
			want: "bytes",
		},
		{
			name: "snapshot fingerprint required",
			mutate: func(c *tapecatalog.Catalog) {
				c.Snapshots[0].PayloadFingerprint = ""
			},
			want: "payloadFingerprint",
		},
		{
			name: "snapshot captured at required",
			mutate: func(c *tapecatalog.Catalog) {
				c.Snapshots[0].CapturedAt = time.Time{}
			},
			want: "capturedAt",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			catalog := validCatalog()
			tc.mutate(&catalog)
			err := tapecatalog.Save(t.TempDir(), catalog)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Save error = %v, want error containing %q", err, tc.want)
			}
			if !strings.HasPrefix(err.Error(), "tapecatalog: ") {
				t.Fatalf("Save error = %q, want tapecatalog prefix", err)
			}
		})
	}
}

func TestLoadValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{
			name:   "tape ID required",
			mutate: func(w map[string]any) { w["tapeID"] = "" },
			want:   "tapeID is required",
		},
		{
			name:   "format ID required",
			mutate: func(w map[string]any) { w["formatID"] = "" },
			want:   "formatID",
		},
		{
			name:   "capacity bytes nonnegative",
			mutate: func(w map[string]any) { w["capacityBytes"] = -1 },
			want:   "capacityBytes must not be negative",
		},
		{
			name:   "free bytes nonnegative",
			mutate: func(w map[string]any) { w["freeBytes"] = -1 },
			want:   "freeBytes",
		},
		{
			name: "free bytes within capacity",
			mutate: func(w map[string]any) {
				w["freeBytes"] = int64(2_500_000_000_001)
			},
			want: "freeBytes",
		},
		{
			name:   "created at required",
			mutate: func(w map[string]any) { w["createdAt"] = "0001-01-01T00:00:00Z" },
			want:   "createdAt",
		},
		{
			name:   "observed at required",
			mutate: func(w map[string]any) { w["observedAt"] = "0001-01-01T00:00:00Z" },
			want:   "observedAt",
		},
		{
			name: "last verified at RFC3339",
			mutate: func(w map[string]any) {
				w["lastVerifiedAt"] = "not-a-time"
			},
			want: "lastVerifiedAt",
		},
		{
			name: "snapshot metadata ref required",
			mutate: func(w map[string]any) {
				firstSnapshot(w)["metadataRef"] = ""
			},
			want: "metadataRef",
		},
		{
			name: "snapshot generation positive",
			mutate: func(w map[string]any) {
				firstSnapshot(w)["generation"] = 0
			},
			want: "generation",
		},
		{
			name: "snapshot bytes nonnegative",
			mutate: func(w map[string]any) {
				firstSnapshot(w)["bytes"] = -1
			},
			want: "bytes",
		},
		{
			name: "snapshot fingerprint required",
			mutate: func(w map[string]any) {
				firstSnapshot(w)["payloadFingerprint"] = ""
			},
			want: "payloadFingerprint",
		},
		{
			name: "snapshot captured at required",
			mutate: func(w map[string]any) {
				firstSnapshot(w)["capturedAt"] = "0001-01-01T00:00:00Z"
			},
			want: "capturedAt",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			wire := validWire()
			tc.mutate(wire)
			writeWire(t, root, "ABC123L6", wire)

			_, err := tapecatalog.Load(root, "ABC123L6")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want error containing %q", err, tc.want)
			}
			if !strings.HasPrefix(err.Error(), "tapecatalog: ") {
				t.Fatalf("Load error = %q, want tapecatalog prefix", err)
			}
		})
	}
}

func TestDuplicateSnapshotRejected(t *testing.T) {
	t.Run("save", func(t *testing.T) {
		catalog := validCatalog()
		catalog.Snapshots = append(catalog.Snapshots, catalog.Snapshots[0])
		err := tapecatalog.Save(t.TempDir(), catalog)
		assertDuplicateError(t, err)
	})

	t.Run("load", func(t *testing.T) {
		root := t.TempDir()
		wire := validWire()
		snapshot := firstSnapshot(wire)
		wire["snapshots"] = append(
			wire["snapshots"].([]any),
			map[string]any{
				"metadataRef":        snapshot["metadataRef"],
				"generation":         snapshot["generation"],
				"bytes":              snapshot["bytes"],
				"payloadFingerprint": snapshot["payloadFingerprint"],
				"capturedAt":         snapshot["capturedAt"],
			},
		)
		writeWire(t, root, "ABC123L6", wire)

		_, err := tapecatalog.Load(root, "ABC123L6")
		assertDuplicateError(t, err)
	})
}

func TestTapeIDFilenameMismatchRejected(t *testing.T) {
	root := t.TempDir()
	writeWire(t, root, "ABC124L6", validWire())

	_, err := tapecatalog.Load(root, "ABC124L6")
	if err == nil ||
		!strings.Contains(err.Error(), "tapeID mismatch") ||
		!strings.Contains(err.Error(), "ABC123L6") ||
		!strings.Contains(err.Error(), "ABC124L6") {
		t.Fatalf("Load error = %v, want mismatch naming both tape IDs", err)
	}
}

func TestPathUnsafeTapeIDsRejected(t *testing.T) {
	tooLong := strings.Repeat("a", 65)
	ids := []tapecatalog.TapeID{
		"",
		".",
		"..",
		"../escape",
		"a/b",
		`a\b`,
		"tape id",
		tapecatalog.TapeID(tooLong),
	}
	for _, id := range ids {
		t.Run(string(id), func(t *testing.T) {
			root := t.TempDir()
			catalog := validCatalog()
			catalog.TapeID = id
			var escapedPath string
			var escapedData []byte
			if id == "../escape" {
				writeWire(t, root, string(id), validWire())
				escapedPath = paths.TapeCatalog(root, string(id))
				var err error
				escapedData, err = os.ReadFile(escapedPath)
				if err != nil {
					t.Fatalf("ReadFile escaped catalog: %v", err)
				}
			}

			want := invalidTapeIDError(id)
			if err := tapecatalog.Save(root, catalog); err == nil || err.Error() != want {
				t.Fatalf("Save error = %v, want %q", err, want)
			}
			if _, err := tapecatalog.Load(root, id); err == nil || err.Error() != want {
				t.Fatalf("Load error = %v, want %q", err, want)
			}
			if escapedPath != "" {
				assertFileUnchanged(t, escapedPath, escapedData)
			}
			if err := tapecatalog.Delete(root, id); err == nil || err.Error() != want {
				t.Fatalf("Delete error = %v, want %q", err, want)
			}
			if escapedPath != "" {
				assertFileUnchanged(t, escapedPath, escapedData)
			}
		})
	}
}

func TestSaveSortsSnapshotsDeterministically(t *testing.T) {
	root := t.TempDir()
	catalog := validCatalog()
	capturedAt := catalog.Snapshots[0].CapturedAt
	catalog.Snapshots = []tapecatalog.Snapshot{
		snapshot("tvdb:2", 2, capturedAt),
		snapshot("tvdb:1", 4, capturedAt),
		snapshot("tvdb:2", 1, capturedAt),
	}
	if err := tapecatalog.Save(root, catalog); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	first, err := os.ReadFile(paths.TapeCatalog(root, string(catalog.TapeID)))
	if err != nil {
		t.Fatalf("first ReadFile: %v", err)
	}

	var wire struct {
		Snapshots []struct {
			MetadataRef string `json:"metadataRef"`
			Generation  int    `json:"generation"`
		} `json:"snapshots"`
	}
	if err := json.Unmarshal(first, &wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	gotOrder := make([]string, 0, len(wire.Snapshots))
	for _, got := range wire.Snapshots {
		gotOrder = append(gotOrder, got.MetadataRef+":"+strconv.Itoa(got.Generation))
	}
	wantOrder := []string{"tvdb:1:4", "tvdb:2:1", "tvdb:2:2"}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("snapshot order = %v, want %v", gotOrder, wantOrder)
	}

	catalog.Snapshots[0], catalog.Snapshots[2] = catalog.Snapshots[2], catalog.Snapshots[0]
	if err := tapecatalog.Save(root, catalog); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	second, err := os.ReadFile(paths.TapeCatalog(root, string(catalog.TapeID)))
	if err != nil {
		t.Fatalf("second ReadFile: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("equivalent Saves differ:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := tapecatalog.Load(t.TempDir(), "ABC123L6")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load error = %v, want os.ErrNotExist", err)
	}
}

func TestList(t *testing.T) {
	t.Run("missing directory", func(t *testing.T) {
		got, err := tapecatalog.List(t.TempDir())
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("List = %#v, want empty non-nil slice", got)
		}
	})

	t.Run("sorted IDs and ignored entries", func(t *testing.T) {
		root := t.TempDir()
		first := validCatalog()
		first.TapeID = "ABC003L6"
		second := validCatalog()
		second.TapeID = "ABC001L6"
		if err := tapecatalog.Save(root, first); err != nil {
			t.Fatalf("Save first: %v", err)
		}
		if err := tapecatalog.Save(root, second); err != nil {
			t.Fatalf("Save second: %v", err)
		}
		if err := os.WriteFile(
			filepath.Join(paths.TapeCatalogDir(root), "notes.txt"),
			[]byte("ignore"),
			0o644,
		); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := os.Mkdir(
			filepath.Join(paths.TapeCatalogDir(root), "ABC002L6.json"),
			0o755,
		); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}

		got, err := tapecatalog.List(root)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		want := []tapecatalog.TapeID{"ABC001L6", "ABC003L6"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("List = %v, want %v", got, want)
		}
	})
}

func TestDeleteRetiresTape(t *testing.T) {
	root := t.TempDir()
	catalog := validCatalog()
	if err := tapecatalog.Save(root, catalog); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := tapecatalog.Delete(root, catalog.TapeID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(paths.TapeCatalog(root, string(catalog.TapeID))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("catalog still exists after Delete: %v", err)
	}
	got, err := tapecatalog.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List = %v, want no registered tapes", got)
	}
	if err := tapecatalog.Delete(root, catalog.TapeID); err != nil {
		t.Fatalf("second Delete = %v, want idempotent success", err)
	}
}

func validCatalog() tapecatalog.Catalog {
	return tapecatalog.Catalog{
		TapeID:        "ABC123L6",
		FormatID:      "9f3c1a7e-0000-4000-8000-000000000000",
		CreatedAt:     time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
		ObservedAt:    time.Date(2026, 7, 24, 18, 3, 11, 0, time.UTC),
		CapacityBytes: 2_500_000_000_000,
		FreeBytes:     812_000_000_000,
		Snapshots: []tapecatalog.Snapshot{
			snapshot(
				"tvdb:370070",
				7,
				time.Date(2026, 7, 21, 13, 10, 0, 0, time.UTC),
			),
		},
	}
}

func snapshot(metadataRef string, generation int, capturedAt time.Time) tapecatalog.Snapshot {
	return tapecatalog.Snapshot{
		MetadataRef:        metadataRef,
		Generation:         generation,
		Bytes:              543_210_000_000,
		PayloadFingerprint: "sha256:0123456789abcdef",
		CapturedAt:         capturedAt,
	}
}

func validWire() map[string]any {
	return map[string]any{
		"schemaVersion": 1,
		"tapeID":        "ABC123L6",
		"formatID":      "9f3c1a7e-0000-4000-8000-000000000000",
		"createdAt":     "2026-07-21T12:00:00Z",
		"observedAt":    "2026-07-24T18:03:11Z",
		"capacityBytes": int64(2_500_000_000_000),
		"freeBytes":     int64(812_000_000_000),
		"snapshots": []any{
			map[string]any{
				"metadataRef":        "tvdb:370070",
				"generation":         7,
				"bytes":              int64(543_210_000_000),
				"payloadFingerprint": "sha256:0123456789abcdef",
				"capturedAt":         "2026-07-21T13:10:00Z",
			},
		},
	}
}

func firstSnapshot(wire map[string]any) map[string]any {
	return wire["snapshots"].([]any)[0].(map[string]any)
}

func writeWire(t *testing.T, root, filenameID string, wire map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	if err := os.MkdirAll(paths.TapeCatalogDir(root), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(
		paths.TapeCatalog(root, filenameID),
		append(data, '\n'),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func invalidTapeIDError(id tapecatalog.TapeID) string {
	value := string(id)
	if value == "" {
		return "tapecatalog: tapeID is required"
	}
	return "tapecatalog: tapeID " + strconv.Quote(value) + " must be exactly 8 characters"
}

func assertFileUnchanged(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile escaped catalog: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("escaped catalog changed:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func assertDuplicateError(t *testing.T, err error) {
	t.Helper()
	if err == nil ||
		!strings.Contains(err.Error(), "duplicate snapshot pair") ||
		!strings.Contains(err.Error(), "tvdb:370070") ||
		!strings.Contains(err.Error(), "7") {
		t.Fatalf("error = %v, want duplicate pair naming tvdb:370070 generation 7", err)
	}
}

package tapecatalog_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/paths"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapecatalog"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

const (
	firstVolumeID  = volume.ID("01J8ZQ7W5TWHA6R6J8X4QZ9Y7V")
	secondVolumeID = volume.ID("01J8ZQ7W5TWHA6R6J8X4QZ9Y7W")
	thirdVolumeID  = volume.ID("01J8ZQ7W5TWHA6R6J8X4QZ9Y7X")
)

func TestRoundTripKeyedByVolumeID(t *testing.T) {
	root := t.TempDir()
	catalog := validCatalog()
	catalog.LastVerifiedAt = time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	catalog.Snapshots[0].Incomplete = true

	if err := tapecatalog.SaveActive(root, catalog); err != nil {
		t.Fatalf("SaveActive: %v", err)
	}
	got, err := tapecatalog.LoadActive(root, catalog.VolumeID)
	if err != nil {
		t.Fatalf("LoadActive: %v", err)
	}
	if !reflect.DeepEqual(got, catalog) {
		t.Fatalf("LoadActive = %#v, want %#v", got, catalog)
	}

	data, err := os.ReadFile(
		paths.ActiveVolumeCatalog(root, string(catalog.VolumeID)),
	)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(data, []byte(`"incomplete": true`)) {
		t.Fatalf("catalog does not contain incomplete marker:\n%s", data)
	}
	if bytes.Contains(data, []byte(`"formatID"`)) {
		t.Fatalf("catalog contains obsolete formatID:\n%s", data)
	}
}

func TestBlankRegisteredVolumeRoundTrip(t *testing.T) {
	root := t.TempDir()
	catalog := validCatalog()
	catalog.FreeBytes = catalog.CapacityBytes
	catalog.LastVerifiedAt = time.Time{}
	catalog.Snapshots = []tapecatalog.Snapshot{}

	if err := tapecatalog.SaveDetached(root, catalog); err != nil {
		t.Fatalf("SaveDetached: %v", err)
	}
	got, err := tapecatalog.LoadDetached(root, catalog.VolumeID)
	if err != nil {
		t.Fatalf("LoadDetached: %v", err)
	}
	if !reflect.DeepEqual(got, catalog) {
		t.Fatalf("LoadDetached = %#v, want %#v", got, catalog)
	}

	data, err := os.ReadFile(
		paths.DetachedVolumeCatalog(root, string(catalog.VolumeID)),
	)
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

func TestTapeIDMayChangeBetweenObservations(t *testing.T) {
	root := t.TempDir()
	catalog := validCatalog()
	if err := tapecatalog.SaveActive(root, catalog); err != nil {
		t.Fatalf("first SaveActive: %v", err)
	}

	catalog.TapeID = "XYZ987L7"
	catalog.ObservedAt = catalog.ObservedAt.Add(time.Hour)
	if err := tapecatalog.SaveActive(root, catalog); err != nil {
		t.Fatalf("second SaveActive: %v", err)
	}
	got, err := tapecatalog.LoadActive(root, catalog.VolumeID)
	if err != nil {
		t.Fatalf("LoadActive: %v", err)
	}
	if !reflect.DeepEqual(got, catalog) {
		t.Fatalf("LoadActive = %#v, want relabeled catalog %#v", got, catalog)
	}
}

func TestUnsupportedSchemaVersion(t *testing.T) {
	root := t.TempDir()
	wire := validWire()
	wire["schemaVersion"] = 2
	writeWire(t, paths.ActiveVolumeCatalog(root, string(firstVolumeID)), wire)

	_, err := tapecatalog.LoadActive(root, firstVolumeID)
	want := "tapecatalog: unsupported schemaVersion 2"
	if err == nil || err.Error() != want {
		t.Fatalf("LoadActive error = %v, want %q", err, want)
	}
}

func TestSaveValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*tapecatalog.Catalog)
		want   string
	}{
		{
			name:   "volume ID required",
			mutate: func(c *tapecatalog.Catalog) { c.VolumeID = "" },
			want:   "tapecatalog: volumeID is required",
		},
		{
			name:   "tape ID required",
			mutate: func(c *tapecatalog.Catalog) { c.TapeID = "" },
			want:   "tapecatalog: tapeID is required",
		},
		{
			name:   "tape ID valid",
			mutate: func(c *tapecatalog.Catalog) { c.TapeID = "ABC123CU" },
			want:   `tapecatalog: tapeID "ABC123CU" identifies a cleaning cartridge`,
		},
		{
			name:   "capacity bytes nonnegative",
			mutate: func(c *tapecatalog.Catalog) { c.CapacityBytes = -1 },
			want:   "tapecatalog: capacityBytes must not be negative",
		},
		{
			name:   "free bytes nonnegative",
			mutate: func(c *tapecatalog.Catalog) { c.FreeBytes = -1 },
			want:   "tapecatalog: freeBytes must not be negative",
		},
		{
			name: "free bytes within capacity",
			mutate: func(c *tapecatalog.Catalog) {
				c.FreeBytes = c.CapacityBytes + 1
			},
			want: "tapecatalog: freeBytes must not exceed capacityBytes",
		},
		{
			name:   "created at required",
			mutate: func(c *tapecatalog.Catalog) { c.CreatedAt = time.Time{} },
			want:   "tapecatalog: createdAt is required",
		},
		{
			name:   "observed at required",
			mutate: func(c *tapecatalog.Catalog) { c.ObservedAt = time.Time{} },
			want:   "tapecatalog: observedAt is required",
		},
		{
			name: "snapshot metadata ref required",
			mutate: func(c *tapecatalog.Catalog) {
				c.Snapshots[0].MetadataRef = ""
			},
			want: "tapecatalog: snapshot metadataRef is required",
		},
		{
			name: "snapshot generation positive",
			mutate: func(c *tapecatalog.Catalog) {
				c.Snapshots[0].Generation = 0
			},
			want: `tapecatalog: snapshot "tvdb:370070" generation must be at least 1`,
		},
		{
			name: "snapshot bytes nonnegative",
			mutate: func(c *tapecatalog.Catalog) {
				c.Snapshots[0].Bytes = -1
			},
			want: `tapecatalog: snapshot ("tvdb:370070", 7) bytes must not be negative`,
		},
		{
			name: "snapshot fingerprint required",
			mutate: func(c *tapecatalog.Catalog) {
				c.Snapshots[0].PayloadFingerprint = ""
			},
			want: `tapecatalog: snapshot ("tvdb:370070", 7) payloadFingerprint is required`,
		},
		{
			name: "snapshot captured at required",
			mutate: func(c *tapecatalog.Catalog) {
				c.Snapshots[0].CapturedAt = time.Time{}
			},
			want: `tapecatalog: snapshot ("tvdb:370070", 7) capturedAt is required`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			catalog := validCatalog()
			tc.mutate(&catalog)
			err := tapecatalog.SaveActive(t.TempDir(), catalog)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("SaveActive error = %v, want %q", err, tc.want)
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
			name:   "volume ID required",
			mutate: func(w map[string]any) { w["volumeID"] = "" },
			want:   "tapecatalog: volumeID is required",
		},
		{
			name:   "tape ID required",
			mutate: func(w map[string]any) { w["tapeID"] = "" },
			want:   "tapecatalog: tapeID is required",
		},
		{
			name:   "tape ID valid",
			mutate: func(w map[string]any) { w["tapeID"] = "ABC123CU" },
			want:   `tapecatalog: tapeID "ABC123CU" identifies a cleaning cartridge`,
		},
		{
			name:   "capacity bytes nonnegative",
			mutate: func(w map[string]any) { w["capacityBytes"] = -1 },
			want:   "tapecatalog: capacityBytes must not be negative",
		},
		{
			name:   "free bytes nonnegative",
			mutate: func(w map[string]any) { w["freeBytes"] = -1 },
			want:   "tapecatalog: freeBytes must not be negative",
		},
		{
			name: "free bytes within capacity",
			mutate: func(w map[string]any) {
				w["freeBytes"] = int64(2_500_000_000_001)
			},
			want: "tapecatalog: freeBytes must not exceed capacityBytes",
		},
		{
			name: "created at required",
			mutate: func(w map[string]any) {
				w["createdAt"] = "0001-01-01T00:00:00Z"
			},
			want: "tapecatalog: createdAt is required",
		},
		{
			name: "observed at required",
			mutate: func(w map[string]any) {
				w["observedAt"] = "0001-01-01T00:00:00Z"
			},
			want: "tapecatalog: observedAt is required",
		},
		{
			name: "last verified at RFC3339",
			mutate: func(w map[string]any) {
				w["lastVerifiedAt"] = "not-a-time"
			},
			want: `tapecatalog: parse lastVerifiedAt: parsing time "not-a-time" as "2006-01-02T15:04:05Z07:00": cannot parse "not-a-time" as "2006"`,
		},
		{
			name: "snapshot metadata ref required",
			mutate: func(w map[string]any) {
				firstSnapshot(w)["metadataRef"] = ""
			},
			want: "tapecatalog: snapshot metadataRef is required",
		},
		{
			name: "snapshot generation positive",
			mutate: func(w map[string]any) {
				firstSnapshot(w)["generation"] = 0
			},
			want: `tapecatalog: snapshot "tvdb:370070" generation must be at least 1`,
		},
		{
			name: "snapshot bytes nonnegative",
			mutate: func(w map[string]any) {
				firstSnapshot(w)["bytes"] = -1
			},
			want: `tapecatalog: snapshot ("tvdb:370070", 7) bytes must not be negative`,
		},
		{
			name: "snapshot fingerprint required",
			mutate: func(w map[string]any) {
				firstSnapshot(w)["payloadFingerprint"] = ""
			},
			want: `tapecatalog: snapshot ("tvdb:370070", 7) payloadFingerprint is required`,
		},
		{
			name: "snapshot captured at required",
			mutate: func(w map[string]any) {
				firstSnapshot(w)["capturedAt"] = "0001-01-01T00:00:00Z"
			},
			want: `tapecatalog: snapshot ("tvdb:370070", 7) capturedAt is required`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			wire := validWire()
			tc.mutate(wire)
			writeWire(
				t,
				paths.ActiveVolumeCatalog(root, string(firstVolumeID)),
				wire,
			)

			_, err := tapecatalog.LoadActive(root, firstVolumeID)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("LoadActive error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestDuplicateSnapshotRejected(t *testing.T) {
	const want = `tapecatalog: duplicate snapshot pair ("tvdb:370070", 7)`

	t.Run("save", func(t *testing.T) {
		catalog := validCatalog()
		catalog.Snapshots = append(catalog.Snapshots, catalog.Snapshots[0])
		err := tapecatalog.SaveActive(t.TempDir(), catalog)
		if err == nil || err.Error() != want {
			t.Fatalf("SaveActive error = %v, want %q", err, want)
		}
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
		writeWire(
			t,
			paths.ActiveVolumeCatalog(root, string(firstVolumeID)),
			wire,
		)

		_, err := tapecatalog.LoadActive(root, firstVolumeID)
		if err == nil || err.Error() != want {
			t.Fatalf("LoadActive error = %v, want %q", err, want)
		}
	})
}

func TestVolumeIDFilenameMismatchRejected(t *testing.T) {
	root := t.TempDir()
	writeWire(
		t,
		paths.ActiveVolumeCatalog(root, string(secondVolumeID)),
		validWire(),
	)

	_, err := tapecatalog.LoadActive(root, secondVolumeID)
	want := `tapecatalog: volumeID mismatch: filename is "01J8ZQ7W5TWHA6R6J8X4QZ9Y7W", file contains "01J8ZQ7W5TWHA6R6J8X4QZ9Y7V"`
	if err == nil || err.Error() != want {
		t.Fatalf("LoadActive error = %v, want %q", err, want)
	}
}

func TestPathUnsafeVolumeIDsRejected(t *testing.T) {
	ids := []volume.ID{
		"",
		".",
		"..",
		"../escape",
		"a/b",
		`a\b`,
		"volume id",
		"01j8zq7w5twha6r6j8x4qz9y7v",
		"01J8ZQ7W5TWHA6R6J8X4QZ9Y7I",
	}
	for _, id := range ids {
		t.Run(string(id), func(t *testing.T) {
			root := t.TempDir()
			catalog := validCatalog()
			catalog.VolumeID = id
			var escapedPath string
			var escapedData []byte
			if id == "../escape" {
				escapedPath = paths.ActiveVolumeCatalog(root, string(id))
				if err := os.MkdirAll(filepath.Dir(escapedPath), 0o755); err != nil {
					t.Fatalf("MkdirAll escaped catalog: %v", err)
				}
				escapedData = []byte("do not overwrite")
				if err := os.WriteFile(escapedPath, escapedData, 0o644); err != nil {
					t.Fatalf("WriteFile escaped catalog: %v", err)
				}
			}

			want := invalidVolumeIDError(id)
			assertExactError(
				t,
				tapecatalog.SaveActive(root, catalog),
				want,
			)
			_, err := tapecatalog.LoadActive(root, id)
			assertExactError(t, err, want)
			_, err = tapecatalog.LoadDetached(root, id)
			assertExactError(t, err, want)
			assertExactError(t, tapecatalog.Detach(root, id), want)
			assertExactError(t, tapecatalog.Attach(root, id), want)
			assertExactError(t, tapecatalog.Purge(root, id), want)
			if escapedPath != "" {
				assertFileEquals(t, escapedPath, escapedData)
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
	if err := tapecatalog.SaveActive(root, catalog); err != nil {
		t.Fatalf("first SaveActive: %v", err)
	}
	path := paths.ActiveVolumeCatalog(root, string(catalog.VolumeID))
	first, err := os.ReadFile(path)
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
		gotOrder = append(
			gotOrder,
			got.MetadataRef+":"+strconv.Itoa(got.Generation),
		)
	}
	wantOrder := []string{"tvdb:1:4", "tvdb:2:1", "tvdb:2:2"}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("snapshot order = %v, want %v", gotOrder, wantOrder)
	}

	catalog.Snapshots[0], catalog.Snapshots[2] =
		catalog.Snapshots[2], catalog.Snapshots[0]
	if err := tapecatalog.SaveActive(root, catalog); err != nil {
		t.Fatalf("second SaveActive: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("second ReadFile: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("equivalent saves differ:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestLoadSortsSnapshotsDeterministically(t *testing.T) {
	root := t.TempDir()
	wire := validWire()
	wireSnapshot := func(metadataRef string, generation int) map[string]any {
		return map[string]any{
			"metadataRef":        metadataRef,
			"generation":         generation,
			"bytes":              int64(543_210_000_000),
			"payloadFingerprint": "sha256:0123456789abcdef",
			"capturedAt":         "2026-07-21T13:10:00Z",
		}
	}
	wire["snapshots"] = []any{
		wireSnapshot("tvdb:2", 2),
		wireSnapshot("tvdb:1", 4),
		wireSnapshot("tvdb:2", 1),
	}
	writeWire(
		t,
		paths.ActiveVolumeCatalog(root, string(firstVolumeID)),
		wire,
	)

	catalog, err := tapecatalog.LoadActive(root, firstVolumeID)
	if err != nil {
		t.Fatalf("LoadActive: %v", err)
	}
	gotOrder := make([]string, 0, len(catalog.Snapshots))
	for _, snapshot := range catalog.Snapshots {
		gotOrder = append(
			gotOrder,
			snapshot.MetadataRef+":"+strconv.Itoa(snapshot.Generation),
		)
	}
	wantOrder := []string{"tvdb:1:4", "tvdb:2:1", "tvdb:2:2"}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("snapshot order = %v, want %v", gotOrder, wantOrder)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := tapecatalog.LoadActive(t.TempDir(), firstVolumeID)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadActive error = %v, want os.ErrNotExist", err)
	}
}

func TestListActiveAndDetachedAreDisjoint(t *testing.T) {
	root := t.TempDir()
	activeFirst := validCatalog()
	activeFirst.VolumeID = thirdVolumeID
	activeSecond := validCatalog()
	activeSecond.VolumeID = firstVolumeID
	detached := validCatalog()
	detached.VolumeID = secondVolumeID

	if err := tapecatalog.SaveActive(root, activeFirst); err != nil {
		t.Fatalf("SaveActive first: %v", err)
	}
	if err := tapecatalog.SaveActive(root, activeSecond); err != nil {
		t.Fatalf("SaveActive second: %v", err)
	}
	if err := tapecatalog.SaveDetached(root, detached); err != nil {
		t.Fatalf("SaveDetached: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(paths.ActiveVolumeCatalogDir(root), "notes.txt"),
		[]byte("ignore"),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Mkdir(
		filepath.Join(
			paths.ActiveVolumeCatalogDir(root),
			string(secondVolumeID)+paths.VolumeCatalogExtension,
		),
		0o755,
	); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	active, err := tapecatalog.ListActive(root)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	wantActive := []volume.ID{firstVolumeID, thirdVolumeID}
	if !reflect.DeepEqual(active, wantActive) {
		t.Fatalf("ListActive = %v, want %v", active, wantActive)
	}

	gotDetached, err := tapecatalog.ListDetached(root)
	if err != nil {
		t.Fatalf("ListDetached: %v", err)
	}
	wantDetached := []volume.ID{secondVolumeID}
	if !reflect.DeepEqual(gotDetached, wantDetached) {
		t.Fatalf("ListDetached = %v, want %v", gotDetached, wantDetached)
	}
}

func TestListIsolationWhenDetachedDirectoryIsAbsent(t *testing.T) {
	root := t.TempDir()
	catalog := validCatalog()
	if err := tapecatalog.SaveActive(root, catalog); err != nil {
		t.Fatalf("SaveActive: %v", err)
	}
	if _, err := os.Stat(paths.DetachedVolumeCatalogDir(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("detached directory exists: %v", err)
	}

	detached, err := tapecatalog.ListDetached(root)
	if err != nil {
		t.Fatalf("ListDetached: %v", err)
	}
	if detached == nil || len(detached) != 0 {
		t.Fatalf("ListDetached = %#v, want empty non-nil slice", detached)
	}
	active, err := tapecatalog.ListActive(root)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	wantActive := []volume.ID{firstVolumeID}
	if !reflect.DeepEqual(active, wantActive) {
		t.Fatalf("ListActive = %v, want %v", active, wantActive)
	}
}

func TestListMissingDirectories(t *testing.T) {
	root := t.TempDir()
	active, err := tapecatalog.ListActive(root)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if active == nil || len(active) != 0 {
		t.Fatalf("ListActive = %#v, want empty non-nil slice", active)
	}
	detached, err := tapecatalog.ListDetached(root)
	if err != nil {
		t.Fatalf("ListDetached: %v", err)
	}
	if detached == nil || len(detached) != 0 {
		t.Fatalf("ListDetached = %#v, want empty non-nil slice", detached)
	}
}

func TestDetachAttachRoundTrip(t *testing.T) {
	root := t.TempDir()
	catalog := validCatalog()
	if err := tapecatalog.SaveActive(root, catalog); err != nil {
		t.Fatalf("SaveActive: %v", err)
	}
	original, err := os.ReadFile(
		paths.ActiveVolumeCatalog(root, string(catalog.VolumeID)),
	)
	if err != nil {
		t.Fatalf("ReadFile active: %v", err)
	}

	if err := tapecatalog.Detach(root, catalog.VolumeID); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if _, err := os.Stat(
		paths.ActiveVolumeCatalog(root, string(catalog.VolumeID)),
	); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("active catalog still exists after Detach: %v", err)
	}
	assertFileEquals(
		t,
		paths.DetachedVolumeCatalog(root, string(catalog.VolumeID)),
		original,
	)

	if err := tapecatalog.Attach(root, catalog.VolumeID); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if _, err := os.Stat(
		paths.DetachedVolumeCatalog(root, string(catalog.VolumeID)),
	); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("detached catalog still exists after Attach: %v", err)
	}
	assertFileEquals(
		t,
		paths.ActiveVolumeCatalog(root, string(catalog.VolumeID)),
		original,
	)
}

func TestSaveRefusesSiblingCatalog(t *testing.T) {
	t.Run("active exists", func(t *testing.T) {
		root := t.TempDir()
		catalog := validCatalog()
		if err := tapecatalog.SaveActive(root, catalog); err != nil {
			t.Fatalf("SaveActive: %v", err)
		}
		activePath := paths.ActiveVolumeCatalog(root, string(catalog.VolumeID))
		detachedPath := paths.DetachedVolumeCatalog(root, string(catalog.VolumeID))
		activeBefore, err := os.ReadFile(activePath)
		if err != nil {
			t.Fatalf("ReadFile active: %v", err)
		}

		err = tapecatalog.SaveDetached(root, catalog)
		want := "tapecatalog: save " + string(catalog.VolumeID) +
			": catalog already exists at " + strconv.Quote(activePath) +
			"; cannot also save at " + strconv.Quote(detachedPath)
		assertExactError(t, err, want)
		assertFileEquals(t, activePath, activeBefore)
		if _, err := os.Stat(detachedPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("detached catalog exists after refused save: %v", err)
		}
	})

	t.Run("detached exists", func(t *testing.T) {
		root := t.TempDir()
		catalog := validCatalog()
		if err := tapecatalog.SaveDetached(root, catalog); err != nil {
			t.Fatalf("SaveDetached: %v", err)
		}
		activePath := paths.ActiveVolumeCatalog(root, string(catalog.VolumeID))
		detachedPath := paths.DetachedVolumeCatalog(root, string(catalog.VolumeID))
		detachedBefore, err := os.ReadFile(detachedPath)
		if err != nil {
			t.Fatalf("ReadFile detached: %v", err)
		}

		err = tapecatalog.SaveActive(root, catalog)
		want := "tapecatalog: save " + string(catalog.VolumeID) +
			": catalog already exists at " + strconv.Quote(detachedPath) +
			"; cannot also save at " + strconv.Quote(activePath)
		assertExactError(t, err, want)
		if _, err := os.Stat(activePath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("active catalog exists after refused save: %v", err)
		}
		assertFileEquals(t, detachedPath, detachedBefore)
	})
}

func TestConcurrentSaveNeverPopulatesBothSets(t *testing.T) {
	const rounds = 200

	root := t.TempDir()
	for round := range rounds {
		roundRoot := filepath.Join(root, strconv.Itoa(round))
		catalog := validCatalog()
		start := make(chan struct{})
		results := make(chan error, 2)
		var group sync.WaitGroup

		group.Go(func() {
			<-start
			results <- tapecatalog.SaveActive(roundRoot, catalog)
		})
		group.Go(func() {
			<-start
			results <- tapecatalog.SaveDetached(roundRoot, catalog)
		})
		close(start)
		group.Wait()
		close(results)

		succeeded := 0
		var saveErrors []error
		for err := range results {
			if err == nil {
				succeeded++
			} else {
				saveErrors = append(saveErrors, err)
			}
		}
		if succeeded == 0 {
			t.Fatalf("round %d: both saves failed: %v", round, saveErrors)
		}

		activePath := paths.ActiveVolumeCatalog(
			roundRoot,
			string(catalog.VolumeID),
		)
		detachedPath := paths.DetachedVolumeCatalog(
			roundRoot,
			string(catalog.VolumeID),
		)
		activeExists := fileExists(t, activePath)
		detachedExists := fileExists(t, detachedPath)
		if activeExists && detachedExists {
			t.Fatalf(
				"round %d: volume %s found in both places: %q and %q",
				round,
				catalog.VolumeID,
				activePath,
				detachedPath,
			)
		}
	}
}

func TestDetachAbsentVolumeErrors(t *testing.T) {
	err := tapecatalog.Detach(t.TempDir(), firstVolumeID)
	want := "tapecatalog: detach 01J8ZQ7W5TWHA6R6J8X4QZ9Y7V: active catalog does not exist"
	if err == nil || err.Error() != want {
		t.Fatalf("Detach error = %v, want %q", err, want)
	}
}

func TestAttachAbsentVolumeErrors(t *testing.T) {
	err := tapecatalog.Attach(t.TempDir(), firstVolumeID)
	want := "tapecatalog: attach 01J8ZQ7W5TWHA6R6J8X4QZ9Y7V: detached catalog does not exist"
	if err == nil || err.Error() != want {
		t.Fatalf("Attach error = %v, want %q", err, want)
	}
}

func TestDetachRefusesExistingDestination(t *testing.T) {
	root := t.TempDir()
	activePath := paths.ActiveVolumeCatalog(root, string(firstVolumeID))
	detachedPath := paths.DetachedVolumeCatalog(root, string(firstVolumeID))
	writeWire(t, activePath, validWire())
	detachedWire := validWire()
	detachedWire["tapeID"] = "XYZ987L7"
	writeWire(t, detachedPath, detachedWire)
	activeBefore, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatalf("ReadFile active: %v", err)
	}
	detachedBefore, err := os.ReadFile(detachedPath)
	if err != nil {
		t.Fatalf("ReadFile detached: %v", err)
	}

	err = tapecatalog.Detach(root, firstVolumeID)
	want := "tapecatalog: detach 01J8ZQ7W5TWHA6R6J8X4QZ9Y7V: detached catalog already exists"
	if err == nil || err.Error() != want {
		t.Fatalf("Detach error = %v, want %q", err, want)
	}
	assertFileEquals(t, activePath, activeBefore)
	assertFileEquals(t, detachedPath, detachedBefore)
}

func TestAttachRefusesExistingDestination(t *testing.T) {
	root := t.TempDir()
	activePath := paths.ActiveVolumeCatalog(root, string(firstVolumeID))
	detachedPath := paths.DetachedVolumeCatalog(root, string(firstVolumeID))
	writeWire(t, activePath, validWire())
	detachedWire := validWire()
	detachedWire["tapeID"] = "XYZ987L7"
	writeWire(t, detachedPath, detachedWire)
	activeBefore, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatalf("ReadFile active: %v", err)
	}
	detachedBefore, err := os.ReadFile(detachedPath)
	if err != nil {
		t.Fatalf("ReadFile detached: %v", err)
	}

	err = tapecatalog.Attach(root, firstVolumeID)
	want := "tapecatalog: attach 01J8ZQ7W5TWHA6R6J8X4QZ9Y7V: active catalog already exists"
	if err == nil || err.Error() != want {
		t.Fatalf("Attach error = %v, want %q", err, want)
	}
	assertFileEquals(t, activePath, activeBefore)
	assertFileEquals(t, detachedPath, detachedBefore)
}

func TestPurgeFromEachLocation(t *testing.T) {
	t.Run("active", func(t *testing.T) {
		root := t.TempDir()
		catalog := validCatalog()
		if err := tapecatalog.SaveActive(root, catalog); err != nil {
			t.Fatalf("SaveActive: %v", err)
		}
		if err := tapecatalog.Purge(root, catalog.VolumeID); err != nil {
			t.Fatalf("Purge: %v", err)
		}
		if _, err := os.Stat(
			paths.ActiveVolumeCatalog(root, string(catalog.VolumeID)),
		); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("active catalog still exists after Purge: %v", err)
		}
	})

	t.Run("detached", func(t *testing.T) {
		root := t.TempDir()
		catalog := validCatalog()
		if err := tapecatalog.SaveDetached(root, catalog); err != nil {
			t.Fatalf("SaveDetached: %v", err)
		}
		if err := tapecatalog.Purge(root, catalog.VolumeID); err != nil {
			t.Fatalf("Purge: %v", err)
		}
		if _, err := os.Stat(
			paths.DetachedVolumeCatalog(root, string(catalog.VolumeID)),
		); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("detached catalog still exists after Purge: %v", err)
		}
	})

	t.Run("already absent", func(t *testing.T) {
		if err := tapecatalog.Purge(t.TempDir(), firstVolumeID); err != nil {
			t.Fatalf("Purge absent volume: %v", err)
		}
	})
}

func TestPurgeBestEffortAcrossLocations(t *testing.T) {
	t.Run("continues after active failure", func(t *testing.T) {
		root := t.TempDir()
		activePath := paths.ActiveVolumeCatalog(root, string(firstVolumeID))
		if err := os.MkdirAll(activePath, 0o755); err != nil {
			t.Fatalf("MkdirAll active path: %v", err)
		}
		if err := os.WriteFile(
			filepath.Join(activePath, "blocker"),
			[]byte("block removal"),
			0o644,
		); err != nil {
			t.Fatalf("WriteFile active blocker: %v", err)
		}
		detachedPath := paths.DetachedVolumeCatalog(root, string(firstVolumeID))
		writeWire(t, detachedPath, validWire())
		removeErr := os.Remove(activePath)
		if removeErr == nil {
			t.Fatal("Remove active path unexpectedly succeeded")
		}

		err := tapecatalog.Purge(root, firstVolumeID)
		want := "tapecatalog: purge " + string(firstVolumeID) +
			": " + removeErr.Error()
		assertExactError(t, err, want)
		if _, err := os.Stat(detachedPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("detached catalog still exists after Purge: %v", err)
		}
	})

	t.Run("joins both failures", func(t *testing.T) {
		root := t.TempDir()
		pathsToBlock := []string{
			paths.ActiveVolumeCatalog(root, string(firstVolumeID)),
			paths.DetachedVolumeCatalog(root, string(firstVolumeID)),
		}
		removeErrors := make([]error, 0, len(pathsToBlock))
		for _, path := range pathsToBlock {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatalf("MkdirAll %s: %v", path, err)
			}
			if err := os.WriteFile(
				filepath.Join(path, "blocker"),
				[]byte("block removal"),
				0o644,
			); err != nil {
				t.Fatalf("WriteFile blocker %s: %v", path, err)
			}
			removeErr := os.Remove(path)
			if removeErr == nil {
				t.Fatalf("Remove %s unexpectedly succeeded", path)
			}
			removeErrors = append(removeErrors, removeErr)
		}

		err := tapecatalog.Purge(root, firstVolumeID)
		want := "tapecatalog: purge " + string(firstVolumeID) +
			": " + removeErrors[0].Error() + "\n" +
			"tapecatalog: purge " + string(firstVolumeID) +
			": " + removeErrors[1].Error()
		assertExactError(t, err, want)
	})
}

func validCatalog() tapecatalog.Catalog {
	return tapecatalog.Catalog{
		VolumeID:      firstVolumeID,
		TapeID:        "ABC123L6",
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

func snapshot(
	metadataRef string,
	generation int,
	capturedAt time.Time,
) tapecatalog.Snapshot {
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
		"volumeID":      string(firstVolumeID),
		"tapeID":        "ABC123L6",
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

func writeWire(t *testing.T, path string, wire map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func invalidVolumeIDError(id volume.ID) string {
	if id == "" {
		return "tapecatalog: volumeID is required"
	}
	return "tapecatalog: volumeID " + strconv.Quote(string(id)) +
		" must be a 26-character uppercase Crockford base32 ULID"
}

func assertExactError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func assertFileEquals(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("file %s changed:\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	t.Fatalf("Lstat %s: %v", path, err)
	return false
}

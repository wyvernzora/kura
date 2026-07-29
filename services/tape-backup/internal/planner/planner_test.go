package planner_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/fingerprint"
	"github.com/wyvernzora/kura/services/tape-backup/internal/planner"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapecatalog"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapemanifest"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

const (
	firstVolumeID  = volume.ID("01J8ZQ7W5TWHA6R6J8X4QZ9Y7V")
	secondVolumeID = volume.ID("01J8ZQ7W5TWHA6R6J8X4QZ9Y7W")
)

func TestReadLibrarySnapshotUsesRealSeriesMetadataAndEligibilityBuckets(t *testing.T) {
	root := t.TempDir()
	eligibleMetadata := writeSeries(
		t,
		root,
		"Eligible",
		`{"schemaVersion":3,"generation":4,"metadataRef":"tvdb:eligible","episodes":{}}`,
	)
	writeFile(t, filepath.Join(root, "Eligible", "episode.mkv"), []byte("data"))
	writeSeries(
		t,
		root,
		"Staged",
		`{"schemaVersion":3,"generation":2,"metadataRef":"tvdb:staged","episodes":{"S01E01":{"staged":{}}}}`,
	)
	writeSeries(
		t,
		root,
		"Claimed",
		`{"schemaVersion":3,"generation":3,"metadataRef":"tvdb:claimed","episodes":{},"in_progress":{}}`,
	)
	writeSeries(
		t,
		root,
		"NonNFC",
		`{"schemaVersion":3,"generation":5,"metadataRef":"tvdb:nfd","episodes":{}}`,
	)
	nfdName := "Cafe\u0301.ass"
	writeFile(t, filepath.Join(root, "NonNFC", nfdName), []byte("subtitle"))
	writeSeries(
		t,
		root,
		"Symlink",
		`{"schemaVersion":3,"generation":6,"metadataRef":"tvdb:symlink","episodes":{}}`,
	)
	mustSucceed(
		t,
		os.Symlink(
			filepath.Join(root, "Eligible", "episode.mkv"),
			filepath.Join(root, "Symlink", "episode.mkv"),
		),
	)
	mustSucceed(t, os.Mkdir(filepath.Join(root, "Untracked"), 0o755))

	digest, err := fingerprint.ComputeExcludingKura(filepath.Join(root, "Eligible"))
	mustSucceed(t, err)
	got, err := planner.ReadLibrarySnapshot(root, 0)
	mustSucceed(t, err)

	want := planner.LibrarySnapshot{Series: []planner.Series{
		{
			MetadataRef: "tvdb:claimed",
			RootPath:    "Claimed",
			Generation:  3,
			Eligibility: planner.EligibilityDeferred,
			Reason:      planner.ReasonActiveClaim,
		},
		{
			MetadataRef:        "tvdb:eligible",
			RootPath:           "Eligible",
			Generation:         4,
			PayloadFingerprint: string(digest),
			Bytes:              int64(len(eligibleMetadata) + len("data")),
			Eligibility:        planner.EligibilityEligible,
		},
		{
			MetadataRef:        "tvdb:nfd",
			RootPath:           "NonNFC",
			Generation:         5,
			PayloadFingerprint: mustFingerprint(t, filepath.Join(root, "NonNFC")),
			Eligibility:        planner.EligibilityUnbackupable,
			Reason:             planner.ReasonNonNFCPath,
			Detail:             "planner: non-nfc path: \"Café.ass\"",
		},
		{
			MetadataRef: "tvdb:staged",
			RootPath:    "Staged",
			Generation:  2,
			Eligibility: planner.EligibilityDeferred,
			Reason:      planner.ReasonStagedIntent,
		},
		{
			MetadataRef: "tvdb:symlink",
			RootPath:    "Symlink",
			Generation:  6,
			Eligibility: planner.EligibilityUnbackupable,
			Reason:      planner.ReasonSymlink,
			Detail:      `fingerprint: symlink "episode.mkv" is not supported`,
		},
	}}
	assertEqual(t, got, want)
}

func TestReadLibrarySnapshotClassifiesSeriesLargerThanSupportedMedia(t *testing.T) {
	root := t.TempDir()
	metadata := writeSeries(
		t,
		root,
		"Large",
		`{"schemaVersion":3,"generation":1,"metadataRef":"tvdb:large","episodes":{}}`,
	)
	writeFile(t, filepath.Join(root, "Large", "episode.mkv"), []byte("x"))
	largest, err := planner.NominalCapacity("ABC123LA")
	mustSucceed(t, err)
	digest, err := fingerprint.ComputeExcludingKura(filepath.Join(root, "Large"))
	mustSucceed(t, err)

	got, err := planner.ReadLibrarySnapshot(root, largest)
	mustSucceed(t, err)
	want := planner.LibrarySnapshot{Series: []planner.Series{{
		MetadataRef:        "tvdb:large",
		RootPath:           "Large",
		Generation:         1,
		PayloadFingerprint: string(digest),
		Bytes:              int64(len(metadata) + 1),
		Eligibility:        planner.EligibilityUnbackupable,
		Reason:             planner.ReasonTooLarge,
		Detail: "series needs 77 bytes plus 30000000000000-byte margin; " +
			"largest cartridge is 30000000000000 bytes",
	}}}
	assertEqual(t, got, want)
}

func TestReadCatalogSnapshotUsesRealCatalogAndCommittedManifestFormats(t *testing.T) {
	stateRoot := t.TempDir()
	installVolume(t, stateRoot, secondVolumeID, "BBB123L6", 2_500, 900)
	installVolume(t, stateRoot, firstVolumeID, "AAA123L6", 2_500, 800)
	putSnapshot(t, stateRoot, firstVolumeID, "tvdb:one", 3, 15, true)
	putSnapshot(t, stateRoot, firstVolumeID, "tvdb:incomplete", 1, 20, false)
	putSnapshot(t, stateRoot, secondVolumeID, "tvdb:two", 7, 25, true)

	got, err := planner.ReadCatalogSnapshot(stateRoot)
	mustSucceed(t, err)
	want := planner.CatalogSnapshot{Volumes: []planner.Volume{
		{
			VolumeID:      firstVolumeID,
			TapeID:        "AAA123L6",
			CapacityBytes: 2_500,
			FreeBytes:     800,
			Snapshots: []planner.Snapshot{{
				MetadataRef:        "tvdb:one",
				Generation:         3,
				TotalBytes:         15,
				PayloadFingerprint: manifestFingerprint(t, 15),
			}},
		},
		{
			VolumeID:      secondVolumeID,
			TapeID:        "BBB123L6",
			CapacityBytes: 2_500,
			FreeBytes:     900,
			Snapshots: []planner.Snapshot{{
				MetadataRef:        "tvdb:two",
				Generation:         7,
				TotalBytes:         25,
				PayloadFingerprint: manifestFingerprint(t, 25),
			}},
		},
	}}
	assertEqual(t, got, want)
}

func TestNominalCapacityCoversEverySupportedMediaIdentifier(t *testing.T) {
	tests := []struct {
		name string
		id   tape.ID
		want int64
	}{
		{name: "lto-1", id: "ABC123L1", want: 100_000_000_000},
		{name: "lto-2", id: "ABC123L2", want: 200_000_000_000},
		{name: "lto-3", id: "ABC123L3", want: 400_000_000_000},
		{name: "lto-4", id: "ABC123L4", want: 800_000_000_000},
		{name: "lto-5", id: "ABC123L5", want: 1_500_000_000_000},
		{name: "lto-6", id: "ABC123L6", want: 2_500_000_000_000},
		{name: "lto-7", id: "ABC123L7", want: 6_000_000_000_000},
		{name: "lto-7 type m", id: "ABC123M8", want: 9_000_000_000_000},
		{name: "lto-8", id: "ABC123L8", want: 12_000_000_000_000},
		{name: "lto-9", id: "ABC123L9", want: 18_000_000_000_000},
		{name: "lto-10", id: "ABC123LA", want: 30_000_000_000_000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := planner.NominalCapacity(test.id)
			mustSucceed(t, err)
			if got != test.want {
				t.Fatalf("NominalCapacity(%q) = %d, want %d", test.id, got, test.want)
			}
		})
	}
}

func TestConsultPendingSetContainsOnlyEligibleCatalogAbsentPairs(t *testing.T) {
	protected := eligibleSeries("tvdb:protected", 2, 4)
	pending := eligibleSeries("tvdb:pending", 3, 5)
	deferred := eligibleSeries("tvdb:deferred", 1, 6)
	deferred.Eligibility = planner.EligibilityDeferred
	deferred.Reason = planner.ReasonStagedIntent
	catalog := planner.CatalogSnapshot{Volumes: []planner.Volume{{
		VolumeID:      firstVolumeID,
		TapeID:        "AAA123L6",
		CapacityBytes: 10,
		FreeBytes:     10,
		Snapshots: []planner.Snapshot{{
			MetadataRef: "tvdb:protected",
			Generation:  2,
		}},
	}}}

	got, err := planner.Consult(
		librarySnapshot(protected, deferred, pending),
		catalog,
		nil,
		0,
	)
	mustSucceed(t, err)
	assertEqual(t, got.Pending, []planner.Series{pending})
	assertEqual(t, got.Deferred, []planner.Series{deferred})
}

func TestConsultIncumbentStickChangesAssignmentWhenIncumbencyIsRemoved(t *testing.T) {
	library := librarySnapshot(eligibleSeries("tvdb:show", 2, 6))
	catalog := planner.CatalogSnapshot{Volumes: []planner.Volume{
		knownVolume(firstVolumeID, "AAA123L6", 10),
		{
			VolumeID:      secondVolumeID,
			TapeID:        "BBB123L6",
			CapacityBytes: 10,
			FreeBytes:     10,
			Snapshots: []planner.Snapshot{{
				MetadataRef: "tvdb:show",
				Generation:  1,
			}},
		},
	}}

	got, err := planner.Consult(library, catalog, nil, 0)
	mustSucceed(t, err)
	if got.Assignments[0].Target.VolumeID != secondVolumeID {
		t.Fatalf(
			"incumbent assignment volume = %s, want %s",
			got.Assignments[0].Target.VolumeID,
			secondVolumeID,
		)
	}

	catalog.Volumes[1].Snapshots = []planner.Snapshot{}
	withoutIncumbency, err := planner.Consult(library, catalog, nil, 0)
	mustSucceed(t, err)
	if withoutIncumbency.Assignments[0].Target.VolumeID != firstVolumeID {
		t.Fatalf(
			"assignment without incumbency volume = %s, want %s",
			withoutIncumbency.Assignments[0].Target.VolumeID,
			firstVolumeID,
		)
	}
}

func TestConsultLargestFirstFitPreventsSmallSeriesFromTakingOnlyRoom(t *testing.T) {
	small := eligibleSeries("tvdb:a-small", 1, 4)
	large := eligibleSeries("tvdb:z-large", 1, 7)
	got, err := planner.Consult(
		librarySnapshot(small, large),
		planner.CatalogSnapshot{Volumes: []planner.Volume{
			knownVolume(firstVolumeID, "AAA123L6", 10),
		}},
		nil,
		0,
	)
	mustSucceed(t, err)

	wantAssignments := []planner.Assignment{{
		Target: planner.Target{
			Kind:          planner.TargetKnown,
			VolumeID:      firstVolumeID,
			TapeID:        "AAA123L6",
			CapacityBytes: 10,
			FreeBytes:     10,
		},
		Series: []planner.Series{large},
		Bytes:  7,
	}}
	assertEqual(t, got.Assignments, wantAssignments)
	assertEqual(t, got.Shortfall, []planner.Series{small})
}

func TestConsultUsesKnownVolumesBeforeDeclaredBlanks(t *testing.T) {
	fitsKnown := eligibleSeries("tvdb:small", 1, 4)
	needsBlank := eligibleSeries("tvdb:large", 1, 6)
	got, err := planner.Consult(
		librarySnapshot(fitsKnown, needsBlank),
		planner.CatalogSnapshot{Volumes: []planner.Volume{
			knownVolume(firstVolumeID, "AAA123L6", 5),
		}},
		[]planner.Blank{{TapeID: "BLANK1L1"}},
		0,
	)
	mustSucceed(t, err)

	want := []planner.Assignment{
		{
			Target: planner.Target{
				Kind:          planner.TargetKnown,
				VolumeID:      firstVolumeID,
				TapeID:        "AAA123L6",
				CapacityBytes: 5,
				FreeBytes:     5,
			},
			Series: []planner.Series{fitsKnown},
			Bytes:  4,
		},
		{
			Target: planner.Target{
				Kind:          planner.TargetBlank,
				TapeID:        "BLANK1L1",
				CapacityBytes: 100_000_000_000,
				FreeBytes:     100_000_000_000,
			},
			Series: []planner.Series{needsBlank},
			Bytes:  6,
		},
	}
	assertEqual(t, got.Assignments, want)
}

func TestConsultReportsSeriesThatFitsNowhereAsShortfall(t *testing.T) {
	pending := eligibleSeries("tvdb:shortfall", 1, 8)
	got, err := planner.Consult(librarySnapshot(pending), planner.CatalogSnapshot{}, nil, 0)
	mustSucceed(t, err)

	assertEqual(t, got.Assignments, []planner.Assignment{})
	assertEqual(t, got.Shortfall, []planner.Series{pending})
	if got.Sizing.ShortfallBytes != 8 {
		t.Fatalf("Sizing.ShortfallBytes = %d, want 8", got.Sizing.ShortfallBytes)
	}
}

func TestConsultR19SizingMathUsesExactKnownAndNominalBlankRoom(t *testing.T) {
	first := eligibleSeries("tvdb:a", 1, 70_000_000_000)
	second := eligibleSeries("tvdb:b", 1, 70_000_000_000)
	third := eligibleSeries("tvdb:c", 1, 70_000_000_000)
	got, err := planner.Consult(
		librarySnapshot(first, second, third),
		planner.CatalogSnapshot{Volumes: []planner.Volume{
			knownVolume(firstVolumeID, "AAA123L1", 60_000_000_010),
		}},
		[]planner.Blank{{TapeID: "BLANK1L1"}},
		10,
	)
	mustSucceed(t, err)

	want := planner.Sizing{
		PendingBytes:                210_000_000_000,
		KnownRoomBytes:              60_000_000_000,
		DeclaredBlankRoomBytes:      99_999_999_990,
		RosterBytes:                 70_000_000_000,
		ShortfallBytes:              140_000_000_000,
		NominalBlankCapacityBytes:   100_000_000_000,
		NominalBlankUsableBytes:     99_999_999_990,
		NominalBlankMediaGeneration: "LTO-1",
		BringBlanks:                 2,
	}
	assertEqual(t, got.Sizing, want)
}

func TestConsultNeverAllocatesLineageRefusal(t *testing.T) {
	live := eligibleSeries("tvdb:readded", 2, 5)
	catalog := planner.CatalogSnapshot{Volumes: []planner.Volume{{
		VolumeID:      firstVolumeID,
		TapeID:        "AAA123L6",
		CapacityBytes: 20,
		FreeBytes:     20,
		Snapshots: []planner.Snapshot{{
			MetadataRef: "tvdb:readded",
			Generation:  3,
		}},
	}}}
	got, err := planner.Consult(librarySnapshot(live), catalog, nil, 0)
	mustSucceed(t, err)

	assertEqual(t, got.Assignments, []planner.Assignment{})
	assertEqual(t, got.Pending, []planner.Series{})
	assertEqual(t, got.LineageRefusals, []planner.LineageRefusal{{
		Series:                 live,
		CatalogedMaxGeneration: 3,
	}})
}

func TestConsultReportsEligibilityBucketsOnlyForUnprotectedPairs(t *testing.T) {
	deferred := eligibleSeries("tvdb:deferred", 1, 0)
	deferred.Eligibility = planner.EligibilityDeferred
	deferred.Reason = planner.ReasonStagedIntent
	unbackupable := eligibleSeries("tvdb:unbackupable", 1, 0)
	unbackupable.Eligibility = planner.EligibilityUnbackupable
	unbackupable.Reason = planner.ReasonNonNFCPath
	protectedDeferred := eligibleSeries("tvdb:protected", 1, 0)
	protectedDeferred.Eligibility = planner.EligibilityDeferred
	protectedDeferred.Reason = planner.ReasonActiveClaim
	catalog := planner.CatalogSnapshot{Volumes: []planner.Volume{{
		VolumeID:      firstVolumeID,
		TapeID:        "AAA123L6",
		CapacityBytes: 10,
		FreeBytes:     10,
		Snapshots: []planner.Snapshot{{
			MetadataRef: "tvdb:protected",
			Generation:  1,
		}},
	}}}

	got, err := planner.Consult(
		librarySnapshot(unbackupable, protectedDeferred, deferred),
		catalog,
		nil,
		0,
	)
	mustSucceed(t, err)
	assertEqual(t, got.Deferred, []planner.Series{deferred})
	assertEqual(t, got.Unbackupable, []planner.Series{unbackupable})
	assertEqual(t, got.InitPlansAwaitingApproval, []planner.InitPlanAwaitingApproval{})
}

func TestConsultIsDeterministicAcrossInputOrderPermutations(t *testing.T) {
	library := librarySnapshot(
		eligibleSeries("tvdb:c", 2, 3),
		eligibleSeries("tvdb:a", 2, 5),
		eligibleSeries("tvdb:b", 2, 4),
	)
	catalog := planner.CatalogSnapshot{Volumes: []planner.Volume{
		{
			VolumeID:      secondVolumeID,
			TapeID:        "BBB123L6",
			CapacityBytes: 5,
			FreeBytes:     5,
			Snapshots: []planner.Snapshot{
				{MetadataRef: "tvdb:c", Generation: 1},
				{MetadataRef: "tvdb:a", Generation: 1},
			},
		},
		knownVolume(firstVolumeID, "AAA123L6", 5),
	}}
	blanks := []planner.Blank{{TapeID: "BLANK2L1"}, {TapeID: "BLANK1L1"}}

	want, err := planner.Consult(library, catalog, blanks, 0)
	mustSucceed(t, err)
	repeated, err := planner.Consult(library, catalog, blanks, 0)
	mustSucceed(t, err)
	assertEqual(t, repeated, want)

	slices.Reverse(library.Series)
	slices.Reverse(catalog.Volumes)
	for i := range catalog.Volumes {
		slices.Reverse(catalog.Volumes[i].Snapshots)
	}
	slices.Reverse(blanks)
	permuted, err := planner.Consult(library, catalog, blanks, 0)
	mustSucceed(t, err)
	assertEqual(t, permuted, want)
}

func eligibleSeries(metadataRef string, generation int, bytes int64) planner.Series {
	return planner.Series{
		MetadataRef:        metadataRef,
		RootPath:           strings.TrimPrefix(metadataRef, "tvdb:"),
		Generation:         generation,
		PayloadFingerprint: "sha256:" + strings.Repeat("0", 64),
		Bytes:              bytes,
		Eligibility:        planner.EligibilityEligible,
	}
}

func librarySnapshot(series ...planner.Series) planner.LibrarySnapshot {
	return planner.LibrarySnapshot{Series: series}
}

func knownVolume(id volume.ID, tapeID tape.ID, free int64) planner.Volume {
	return planner.Volume{
		VolumeID:      id,
		TapeID:        tapeID,
		CapacityBytes: free,
		FreeBytes:     free,
		Snapshots:     []planner.Snapshot{},
	}
}

func writeSeries(t *testing.T, root, name, body string) []byte {
	t.Helper()
	data := append([]byte(body), '\n')
	writeFile(t, filepath.Join(root, name, ".kura", "series.json"), data)
	return data
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	mustSucceed(t, os.MkdirAll(filepath.Dir(path), 0o755))
	mustSucceed(t, os.WriteFile(path, data, 0o644))
}

func mustFingerprint(t *testing.T, root string) string {
	t.Helper()
	digest, err := fingerprint.ComputeExcludingKura(root)
	mustSucceed(t, err)
	return string(digest)
}

func installVolume(
	t *testing.T,
	stateRoot string,
	id volume.ID,
	tapeID tape.ID,
	capacity, free int64,
) {
	t.Helper()
	headerRoot := t.TempDir()
	mustSucceed(t, tapevolume.Write(headerRoot, tapevolume.Volume{
		VolumeID:  id,
		TapeID:    tapeID,
		CreatedAt: time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
	}))
	header, err := os.ReadFile(tapevolume.VolumeFile(headerRoot))
	mustSucceed(t, err)
	mustSucceed(t, tapecatalog.InstallVolume(
		stateRoot,
		id,
		header,
		tapecatalog.Observed{
			TapeID:        tapeID,
			ObservedAt:    time.Date(2026, 7, 21, 13, 0, 0, 0, time.UTC),
			CapacityBytes: capacity,
			FreeBytes:     free,
		},
	))
}

func putSnapshot(
	t *testing.T,
	stateRoot string,
	id volume.ID,
	metadataRef string,
	generation int,
	bytes int64,
	complete bool,
) {
	t.Helper()
	root := t.TempDir()
	snapshotDir, err := tapevolume.SnapshotDir(root, metadataRef, generation)
	mustSucceed(t, err)
	manifest := tapemanifest.Manifest{
		MetadataRef: metadataRef,
		RootPath:    "Show",
		Generation:  generation,
		CapturedAt:  time.Date(2026, 7, 21, 13, 10, 0, 0, time.UTC),
		WrittenBy: tapemanifest.Writer{
			Version: "v0.1.0",
			Host:    "tape-vm",
		},
		TotalBytes: bytes,
		Files: []tapemanifest.File{{
			Path:    "episode.mkv",
			Size:    bytes,
			ModTime: time.Date(2026, 7, 21, 13, 0, 0, 0, time.UTC),
			Hash:    "sha256:" + strings.Repeat("a", 64),
		}},
	}
	mustSucceed(t, tapemanifest.Write(snapshotDir, manifest))
	manifestData, err := os.ReadFile(filepath.Join(snapshotDir, "manifest.json"))
	mustSucceed(t, err)
	completeData := []byte(nil)
	if complete {
		mustSucceed(t, tapemanifest.Commit(snapshotDir))
		completeData, err = os.ReadFile(filepath.Join(snapshotDir, "complete.json"))
		mustSucceed(t, err)
	}
	name := filepath.Base(snapshotDir)
	mustSucceed(t, tapecatalog.PutSnapshot(
		stateRoot,
		id,
		name,
		manifestData,
		completeData,
	))
}

func manifestFingerprint(t *testing.T, bytes int64) string {
	t.Helper()
	digest, err := fingerprint.OfExcludingKura([]fingerprint.Entry{{
		Path:    "episode.mkv",
		Size:    bytes,
		ModTime: time.Date(2026, 7, 21, 13, 0, 0, 0, time.UTC),
	}})
	mustSucceed(t, err)
	return string(digest)
}

func mustSucceed(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got:\n%#v\nwant:\n%#v", got, want)
	}
}

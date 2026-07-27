package tapecatalog_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/snapshotname"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/paths"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapecatalog"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapemanifest"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

const (
	firstVolumeID  = volume.ID("01J8ZQ7W5TWHA6R6J8X4QZ9Y7V")
	secondVolumeID = volume.ID("01J8ZQ7W5TWHA6R6J8X4QZ9Y7W")
	thirdVolumeID  = volume.ID("01J8ZQ7W5TWHA6R6J8X4QZ9Y7X")
)

func TestMutatorAlphabetNeverPopulatesBothSets(t *testing.T) {
	root := t.TempDir()
	header := volumeHeaderBytes(t, firstVolumeID)
	name7, manifest7, complete7 := snapshotBytes(t, "tvdb:370070", 7, true, false)
	name8, manifest8, _ := snapshotBytes(t, "tvdb:370070", 8, false, false)

	steps := []struct {
		name string
		run  func() error
	}{
		{
			name: "save observed",
			run: func() error {
				return tapecatalog.SaveObserved(root, firstVolumeID, validObserved())
			},
		},
		{
			name: "put volume header",
			run: func() error {
				return tapecatalog.PutVolumeHeader(root, firstVolumeID, header)
			},
		},
		{
			name: "put complete snapshot",
			run: func() error {
				return tapecatalog.PutSnapshot(
					root,
					firstVolumeID,
					name7,
					manifest7,
					complete7,
				)
			},
		},
		{
			name: "detach",
			run:  func() error { return tapecatalog.Detach(root, firstVolumeID) },
		},
		{
			name: "save observed while detached",
			run: func() error {
				observed := validObserved()
				observed.ObservedAt = observed.ObservedAt.Add(time.Hour)
				return tapecatalog.SaveObserved(root, firstVolumeID, observed)
			},
		},
		{
			name: "replace header while detached",
			run: func() error {
				return tapecatalog.PutVolumeHeader(root, firstVolumeID, header)
			},
		},
		{
			name: "put incomplete snapshot while detached",
			run: func() error {
				return tapecatalog.PutSnapshot(
					root,
					firstVolumeID,
					name8,
					manifest8,
					nil,
				)
			},
		},
		{
			name: "attach",
			run:  func() error { return tapecatalog.Attach(root, firstVolumeID) },
		},
		{
			name: "purge",
			run:  func() error { return tapecatalog.Purge(root, firstVolumeID) },
		},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			mustSucceed(t, step.run())
			assertDisjoint(t, root, firstVolumeID)
		})
	}
}

func TestConcurrentMutatorsNeverPopulateBothSets(t *testing.T) {
	const (
		rounds     = 3
		iterations = 8
	)

	header := volumeHeaderBytes(t, firstVolumeID)
	snapshots := make([]struct {
		name     string
		manifest []byte
		complete []byte
	}, 0, iterations)
	for iteration := range iterations {
		name, manifest, complete := snapshotBytes(
			t,
			"tvdb:370070",
			iteration+1,
			true,
			false,
		)
		snapshots = append(snapshots, struct {
			name     string
			manifest []byte
			complete []byte
		}{
			name:     name,
			manifest: manifest,
			complete: complete,
		})
	}

	mutators := []struct {
		name string
		run  func(string, int) error
	}{
		{
			name: "save observed",
			run: func(root string, iteration int) error {
				observed := validObserved()
				observed.ObservedAt = observed.ObservedAt.Add(
					time.Duration(iteration) * time.Second,
				)
				return tapecatalog.SaveObserved(root, firstVolumeID, observed)
			},
		},
		{
			name: "put volume header",
			run: func(root string, _ int) error {
				return tapecatalog.PutVolumeHeader(root, firstVolumeID, header)
			},
		},
		{
			name: "put snapshot",
			run: func(root string, iteration int) error {
				snapshot := snapshots[iteration]
				return tapecatalog.PutSnapshot(
					root,
					firstVolumeID,
					snapshot.name,
					snapshot.manifest,
					snapshot.complete,
				)
			},
		},
	}

	for _, mutator := range mutators {
		t.Run(mutator.name, func(t *testing.T) {
			for round := range rounds {
				root := filepath.Join(t.TempDir(), strconv.Itoa(round))
				mustSucceed(
					t,
					tapecatalog.SaveObserved(root, firstVolumeID, validObserved()),
				)
				start := make(chan struct{})
				results := make(chan error, iterations*3)
				var group sync.WaitGroup

				group.Go(func() {
					<-start
					for iteration := range iterations {
						results <- mutator.run(root, iteration)
					}
				})
				group.Go(func() {
					<-start
					for range iterations {
						results <- tapecatalog.Detach(root, firstVolumeID)
						results <- tapecatalog.Attach(root, firstVolumeID)
					}
				})

				close(start)
				group.Wait()
				close(results)
				for err := range results {
					mustSucceed(t, err)
				}
				assertDisjoint(t, root, firstVolumeID)
			}
		})
	}
}

func TestConcurrentMoveNeverPopulatesBothSets(t *testing.T) {
	const (
		rounds     = 3
		iterations = 8
	)

	for round := range rounds {
		root := filepath.Join(t.TempDir(), strconv.Itoa(round))
		mustSucceed(
			t,
			tapecatalog.SaveObserved(root, firstVolumeID, validObserved()),
		)
		start := make(chan struct{})
		results := make(chan error, iterations*3)
		var group sync.WaitGroup

		group.Go(func() {
			<-start
			for iteration := range iterations {
				observed := validObserved()
				observed.ObservedAt = observed.ObservedAt.Add(
					time.Duration(iteration) * time.Second,
				)
				results <- tapecatalog.SaveObserved(root, firstVolumeID, observed)
			}
		})
		group.Go(func() {
			<-start
			for range iterations {
				results <- tapecatalog.Detach(root, firstVolumeID)
				results <- tapecatalog.Attach(root, firstVolumeID)
			}
		})

		close(start)
		group.Wait()
		close(results)
		for err := range results {
			mustSucceed(t, err)
		}
		assertDisjoint(t, root, firstVolumeID)
	}
}

func TestConcurrentPurgeAndAttachLeavesNoVolumeAfterSuccessfulPurge(t *testing.T) {
	const (
		rounds      = 40
		attachCount = 8
	)

	for round := range rounds {
		root := filepath.Join(t.TempDir(), strconv.Itoa(round))
		mustSucceed(
			t,
			tapecatalog.SaveObserved(root, firstVolumeID, validObserved()),
		)
		mustSucceed(t, tapecatalog.Detach(root, firstVolumeID))
		start := make(chan struct{})
		purgeResult := make(chan error, 1)
		var group sync.WaitGroup

		group.Go(func() {
			<-start
			purgeResult <- tapecatalog.Purge(root, firstVolumeID)
		})
		for range attachCount {
			group.Go(func() {
				<-start
				_ = tapecatalog.Attach(root, firstVolumeID)
			})
		}

		close(start)
		group.Wait()
		mustSucceed(t, <-purgeResult)
		assertNotExist(t, activeVolumeDir(root, firstVolumeID))
		assertNotExist(t, detachedVolumeDir(root, firstVolumeID))
	}
}

func TestConcurrentReadersNeverMissMovingVolume(t *testing.T) {
	const iterations = 2_000

	readers := []struct {
		name string
		read func(string) error
	}{
		{
			name: "volume dir",
			read: func(root string) error {
				_, err := tapecatalog.VolumeDir(root, firstVolumeID)
				return err
			},
		},
		{
			name: "load observed",
			read: func(root string) error {
				_, err := tapecatalog.LoadObserved(root, firstVolumeID)
				return err
			},
		},
	}

	for _, reader := range readers {
		t.Run(reader.name, func(t *testing.T) {
			root := t.TempDir()
			mustSucceed(
				t,
				tapecatalog.SaveObserved(root, firstVolumeID, validObserved()),
			)
			start := make(chan struct{})
			results := make(chan error, iterations*3)
			var group sync.WaitGroup

			group.Go(func() {
				<-start
				for range iterations {
					results <- reader.read(root)
				}
			})
			group.Go(func() {
				<-start
				for range iterations {
					results <- tapecatalog.Detach(root, firstVolumeID)
					results <- tapecatalog.Attach(root, firstVolumeID)
				}
			})

			close(start)
			group.Wait()
			close(results)
			for err := range results {
				if errors.Is(err, os.ErrNotExist) {
					t.Fatalf("reader reported spurious os.ErrNotExist: %v", err)
				}
				mustSucceed(t, err)
			}
		})
	}
}

func TestMoveRefusesOccupiedDestination(t *testing.T) {
	tests := []struct {
		name           string
		action         string
		sourceSet      string
		destinationSet string
		source         func(string) string
		destination    func(string) string
		move           func(string) error
	}{
		{
			name:           "detach",
			action:         "detach",
			sourceSet:      "active",
			destinationSet: "detached",
			source: func(root string) string {
				return activeVolumeDir(root, firstVolumeID)
			},
			destination: func(root string) string {
				return detachedVolumeDir(root, firstVolumeID)
			},
			move: func(root string) error {
				return tapecatalog.Detach(root, firstVolumeID)
			},
		},
		{
			name:           "attach",
			action:         "attach",
			sourceSet:      "detached",
			destinationSet: "active",
			source: func(root string) string {
				return detachedVolumeDir(root, firstVolumeID)
			},
			destination: func(root string) string {
				return activeVolumeDir(root, firstVolumeID)
			},
			move: func(root string) error {
				return tapecatalog.Attach(root, firstVolumeID)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source := test.source(root)
			destination := test.destination(root)
			writeMarker(t, source, test.sourceSet)
			writeMarker(t, destination, test.destinationSet)

			want := "tapecatalog: " + test.action + " " + string(firstVolumeID) +
				": cannot move from " + strconv.Quote(source) +
				" to " + strconv.Quote(destination) +
				": " + test.destinationSet + " volume already exists"
			assertExactError(t, test.move(root), want)
			assertMarker(t, source, test.sourceSet)
			assertMarker(t, destination, test.destinationSet)
		})
	}
}

func TestMoveRefusesMissingSource(t *testing.T) {
	tests := []struct {
		name      string
		action    string
		sourceSet string
		move      func(string) error
	}{
		{
			name:      "detach",
			action:    "detach",
			sourceSet: "active",
			move: func(root string) error {
				return tapecatalog.Detach(root, firstVolumeID)
			},
		},
		{
			name:      "attach",
			action:    "attach",
			sourceSet: "detached",
			move: func(root string) error {
				return tapecatalog.Attach(root, firstVolumeID)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := "tapecatalog: " + test.action + " " + string(firstVolumeID) +
				": " + test.sourceSet + " volume does not exist"
			assertExactError(t, test.move(t.TempDir()), want)
		})
	}
}

func TestMutatorsRefuseBothPopulatedVolume(t *testing.T) {
	header := volumeHeaderBytes(t, firstVolumeID)
	name, manifest, complete := snapshotBytes(
		t,
		"tvdb:370070",
		7,
		true,
		false,
	)
	mutators := []struct {
		name string
		run  func(string) error
	}{
		{
			name: "save observed",
			run: func(root string) error {
				return tapecatalog.SaveObserved(root, firstVolumeID, validObserved())
			},
		},
		{
			name: "put volume header",
			run: func(root string) error {
				return tapecatalog.PutVolumeHeader(root, firstVolumeID, header)
			},
		},
		{
			name: "put snapshot",
			run: func(root string) error {
				return tapecatalog.PutSnapshot(
					root,
					firstVolumeID,
					name,
					manifest,
					complete,
				)
			},
		},
	}

	for _, mutator := range mutators {
		t.Run(mutator.name, func(t *testing.T) {
			root := t.TempDir()
			active := activeVolumeDir(root, firstVolumeID)
			detached := detachedVolumeDir(root, firstVolumeID)
			writeMarker(t, active, "active")
			writeMarker(t, detached, "detached")

			err := mutator.run(root)
			mustSucceed(t, tapecatalog.Purge(root, firstVolumeID))
			assertNotExist(t, active)
			assertNotExist(t, detached)

			want := "tapecatalog: volume " + string(firstVolumeID) +
				" exists at both " + strconv.Quote(active) +
				" and " + strconv.Quote(detached)
			assertExactError(t, err, want)
		})
	}
}

func TestVolumeLookupRejectsNonDirectories(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string, string)
	}{
		{
			name: "plain file",
			setup: func(t *testing.T, root, active string) {
				t.Helper()
				mustSucceed(t, os.MkdirAll(filepath.Dir(active), 0o775))
				mustSucceed(t, os.WriteFile(active, []byte("not a directory"), 0o664))
			},
		},
		{
			name: "symlink aliases detached volume",
			setup: func(t *testing.T, root, active string) {
				t.Helper()
				detached := detachedVolumeDir(root, firstVolumeID)
				writeMarker(t, detached, "detached")
				mustSucceed(t, os.MkdirAll(filepath.Dir(active), 0o775))
				mustSucceed(t, os.Symlink(detached, active))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			active := activeVolumeDir(root, firstVolumeID)
			test.setup(t, root, active)

			_, err := tapecatalog.VolumeDir(root, firstVolumeID)
			want := "tapecatalog: inspect active volume " + string(firstVolumeID) +
				": " + strconv.Quote(active) + " is not a directory"
			assertExactError(t, err, want)
		})
	}
}

func TestPurgeBestEffortAcrossLocations(t *testing.T) {
	t.Run("continues after first failure", func(t *testing.T) {
		root := t.TempDir()
		detached := detachedVolumeDir(root, firstVolumeID)
		writeMarker(t, detached, "detached")
		activeParent := paths.ActiveVolumeCatalogDir(root)
		mustSucceed(t, os.WriteFile(activeParent, []byte("not a directory"), 0o644))
		active := activeVolumeDir(root, firstVolumeID)
		removeErr := os.RemoveAll(active)
		if !errors.Is(removeErr, syscall.ENOTDIR) {
			t.Fatalf("RemoveAll setup error = %v, want ENOTDIR", removeErr)
		}

		want := "tapecatalog: purge " + string(firstVolumeID) + " at " +
			strconv.Quote(active) + ": " + removeErr.Error()
		assertExactError(t, tapecatalog.Purge(root, firstVolumeID), want)
		assertNotExist(t, detached)
	})

	t.Run("joins failures from both locations", func(t *testing.T) {
		root := t.TempDir()
		mustSucceed(
			t,
			os.MkdirAll(filepath.Join(root, "volumes"), 0o755),
		)
		mustSucceed(
			t,
			os.WriteFile(
				paths.ActiveVolumeCatalogDir(root),
				[]byte("not a directory"),
				0o644,
			),
		)
		mustSucceed(
			t,
			os.WriteFile(
				paths.DetachedVolumeCatalogDir(root),
				[]byte("not a directory"),
				0o644,
			),
		)
		active := activeVolumeDir(root, firstVolumeID)
		detached := detachedVolumeDir(root, firstVolumeID)
		activeErr := os.RemoveAll(active)
		detachedErr := os.RemoveAll(detached)
		if !errors.Is(activeErr, syscall.ENOTDIR) {
			t.Fatalf("active RemoveAll setup error = %v, want ENOTDIR", activeErr)
		}
		if !errors.Is(detachedErr, syscall.ENOTDIR) {
			t.Fatalf("detached RemoveAll setup error = %v, want ENOTDIR", detachedErr)
		}

		want := "tapecatalog: purge " + string(firstVolumeID) + " at " +
			strconv.Quote(active) + ": " + activeErr.Error() + "\n" +
			"tapecatalog: purge " + string(firstVolumeID) + " at " +
			strconv.Quote(detached) + ": " + detachedErr.Error()
		assertExactError(t, tapecatalog.Purge(root, firstVolumeID), want)
	})
}

func TestCreateVolumeTempCleanedAfterFailure(t *testing.T) {
	root := t.TempDir()
	header := volumeHeaderBytes(t, secondVolumeID)
	err := tapecatalog.PutVolumeHeader(root, firstVolumeID, header)
	want := `tapecatalog: volumeID mismatch: catalog is "` +
		string(firstVolumeID) + `", header contains "` + string(secondVolumeID) + `"`
	assertExactError(t, err, want)

	entries, readErr := os.ReadDir(paths.ActiveVolumeCatalogDir(root))
	mustSucceed(t, readErr)
	if len(entries) != 0 {
		t.Fatalf("active directory contains temporary volume entries: %v", entries)
	}
}

func TestVolumeHeaderTempCleanedAfterFailure(t *testing.T) {
	root := t.TempDir()
	mustSucceed(
		t,
		tapecatalog.SaveObserved(root, firstVolumeID, validObserved()),
	)
	header := volumeHeaderBytes(t, secondVolumeID)
	err := tapecatalog.PutVolumeHeader(root, firstVolumeID, header)
	want := `tapecatalog: volumeID mismatch: catalog is "` +
		string(firstVolumeID) + `", header contains "` + string(secondVolumeID) + `"`
	assertExactError(t, err, want)

	entries, readErr := os.ReadDir(activeVolumeDir(root, firstVolumeID))
	mustSucceed(t, readErr)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".header-") {
			t.Fatalf("volume directory contains temporary header %q", entry.Name())
		}
	}
}

func TestSnapshotTempCleanedAfterFailure(t *testing.T) {
	root := t.TempDir()
	mustSucceed(
		t,
		tapecatalog.SaveObserved(root, firstVolumeID, validObserved()),
	)
	volumeDir := activeVolumeDir(root, firstVolumeID)
	done := make(chan struct{})
	staged := make(chan bool, 1)
	go func() {
		for {
			entries, err := os.ReadDir(volumeDir)
			if err != nil {
				staged <- false
				return
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".snapshot-") {
					staged <- true
					return
				}
			}
			select {
			case <-done:
				staged <- false
				return
			default:
				runtime.Gosched()
			}
		}
	}()

	manifest := append([]byte("{}"), bytes.Repeat([]byte(" "), 32<<20)...)
	err := tapecatalog.PutSnapshot(
		root,
		firstVolumeID,
		"OR3GIYR2GM3TAMBXGA.g7",
		manifest,
		nil,
	)
	close(done)
	if !<-staged {
		t.Fatal("temporary snapshot was not staged in the volume directory")
	}
	const want = "tapecatalog: put snapshot OR3GIYR2GM3TAMBXGA.g7: " +
		"tapemanifest: unsupported manifest schemaVersion 0"
	assertExactError(t, err, want)

	entries, readErr := os.ReadDir(volumeDir)
	mustSucceed(t, readErr)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".snapshot-") {
			t.Fatalf("volume directory contains temporary snapshot %q", entry.Name())
		}
	}
}

func TestSnapshotTempCleanupFailureReturned(t *testing.T) {
	root := t.TempDir()
	mustSucceed(
		t,
		tapecatalog.SaveObserved(root, firstVolumeID, validObserved()),
	)
	volumeDir := activeVolumeDir(root, firstVolumeID)
	t.Cleanup(func() {
		_ = os.Chmod(volumeDir, 0o775)
	})

	done := make(chan struct{})
	locked := make(chan struct{})
	go func() {
		for {
			entries, err := os.ReadDir(volumeDir)
			if err != nil {
				runtime.Gosched()
				continue
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".snapshot-") {
					_ = os.Chmod(volumeDir, 0o555)
					close(locked)
					return
				}
			}
			select {
			case <-done:
				close(locked)
				return
			default:
				runtime.Gosched()
			}
		}
	}()

	manifest := append([]byte("{}"), bytes.Repeat([]byte(" "), 32<<20)...)
	err := tapecatalog.PutSnapshot(
		root,
		firstVolumeID,
		"OR3GIYR2GM3TAMBXGA.g7",
		manifest,
		nil,
	)
	close(done)
	<-locked
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("PutSnapshot error = %v, want os.ErrPermission", err)
	}
}

func TestCreatedVolumeDirectoryMode(t *testing.T) {
	root := t.TempDir()
	mustSucceed(
		t,
		tapecatalog.SaveObserved(root, firstVolumeID, validObserved()),
	)
	info, err := os.Stat(activeVolumeDir(root, firstVolumeID))
	mustSucceed(t, err)
	if got, want := info.Mode().Perm(), os.FileMode(0o775); got != want {
		t.Fatalf("volume directory mode = %o, want %o", got, want)
	}
}

func TestPutSnapshotRejectsInvalidBytesWithoutPartialDirectory(t *testing.T) {
	tests := []struct {
		name     string
		manifest func(*testing.T) []byte
		complete func(*testing.T) []byte
		want     string
	}{
		{
			name:     "invalid manifest",
			manifest: func(*testing.T) []byte { return []byte("{}\n") },
			complete: func(*testing.T) []byte { return nil },
			want: "tapecatalog: put snapshot OR3GIYR2GM3TAMBXGA.g7: " +
				"tapemanifest: unsupported manifest schemaVersion 0",
		},
		{
			name: "invalid complete marker",
			manifest: func(t *testing.T) []byte {
				_, manifest, _ := snapshotBytes(
					t,
					"tvdb:370070",
					7,
					false,
					false,
				)
				return manifest
			},
			complete: func(*testing.T) []byte { return []byte("{}\n") },
			want: "tapecatalog: put snapshot OR3GIYR2GM3TAMBXGA.g7: " +
				"tapemanifest: unsupported complete schemaVersion 0",
		},
		{
			name: "empty complete marker is not absence",
			manifest: func(t *testing.T) []byte {
				_, manifest, _ := snapshotBytes(
					t,
					"tvdb:370070",
					7,
					false,
					false,
				)
				return manifest
			},
			complete: func(*testing.T) []byte { return []byte{} },
			want: "tapecatalog: put snapshot OR3GIYR2GM3TAMBXGA.g7: " +
				"tapemanifest: decode complete: unexpected end of JSON input",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			mustSucceed(
				t,
				tapecatalog.SaveObserved(root, firstVolumeID, validObserved()),
			)
			err := tapecatalog.PutSnapshot(
				root,
				firstVolumeID,
				"OR3GIYR2GM3TAMBXGA.g7",
				test.manifest(t),
				test.complete(t),
			)
			assertExactError(t, err, test.want)

			snapshotsDir := tapevolume.SnapshotsDir(
				activeVolumeDir(root, firstVolumeID),
			)
			entries, readErr := os.ReadDir(snapshotsDir)
			mustSucceed(t, readErr)
			if len(entries) != 0 {
				t.Fatalf("snapshot directory contains partial entries: %v", entries)
			}
		})
	}
}

func TestFailedPutSnapshotLeavesOnlyCanonicalSnapshotEntries(t *testing.T) {
	root := t.TempDir()
	mustSucceed(
		t,
		tapecatalog.SaveObserved(root, firstVolumeID, validObserved()),
	)
	snapshotsDir := tapevolume.SnapshotsDir(
		activeVolumeDir(root, firstVolumeID),
	)
	mustSucceed(t, os.MkdirAll(snapshotsDir, 0o775))
	t.Cleanup(func() {
		_ = os.Chmod(snapshotsDir, 0o775)
	})

	done := make(chan struct{})
	observerResult := make(chan error, 1)
	go func() {
		for {
			entries, err := os.ReadDir(snapshotsDir)
			if err != nil {
				observerResult <- err
				return
			}
			for _, entry := range entries {
				if _, _, err := snapshotname.Parse(entry.Name()); err != nil {
					observerResult <- os.Chmod(snapshotsDir, 0o555)
					return
				}
			}
			select {
			case <-done:
				observerResult <- nil
				return
			default:
				runtime.Gosched()
			}
		}
	}()

	manifest := append([]byte("{}"), bytes.Repeat([]byte(" "), 32<<20)...)
	err := tapecatalog.PutSnapshot(
		root,
		firstVolumeID,
		"OR3GIYR2GM3TAMBXGA.g7",
		manifest,
		nil,
	)
	close(done)
	mustSucceed(t, <-observerResult)
	mustSucceed(t, os.Chmod(snapshotsDir, 0o775))
	const want = "tapecatalog: put snapshot OR3GIYR2GM3TAMBXGA.g7: " +
		"tapemanifest: unsupported manifest schemaVersion 0"
	assertExactError(t, err, want)

	entries, readErr := os.ReadDir(snapshotsDir)
	mustSucceed(t, readErr)
	for _, entry := range entries {
		if _, _, parseErr := snapshotname.Parse(entry.Name()); parseErr != nil {
			t.Fatalf(
				"snapshot directory contains non-snapshot entry %q: %v",
				entry.Name(),
				parseErr,
			)
		}
	}
}

func TestPutSnapshotStoresBytesVerbatim(t *testing.T) {
	t.Run("complete", func(t *testing.T) {
		root := t.TempDir()
		name, manifest, complete := snapshotBytes(
			t,
			"tvdb:370070",
			7,
			true,
			true,
		)
		mustSucceed(
			t,
			tapecatalog.PutSnapshot(
				root,
				firstVolumeID,
				name,
				manifest,
				complete,
			),
		)
		snapshotDir, err := tapevolume.SnapshotDir(
			activeVolumeDir(root, firstVolumeID),
			"tvdb:370070",
			7,
		)
		mustSucceed(t, err)
		assertFileBytes(t, filepath.Join(snapshotDir, "manifest.json"), manifest)
		assertFileBytes(t, filepath.Join(snapshotDir, "complete.json"), complete)
	})

	t.Run("incomplete", func(t *testing.T) {
		root := t.TempDir()
		name, manifest, _ := snapshotBytes(
			t,
			"tvdb:370070",
			8,
			false,
			true,
		)
		mustSucceed(
			t,
			tapecatalog.PutSnapshot(
				root,
				firstVolumeID,
				name,
				manifest,
				nil,
			),
		)
		snapshotDir, err := tapevolume.SnapshotDir(
			activeVolumeDir(root, firstVolumeID),
			"tvdb:370070",
			8,
		)
		mustSucceed(t, err)
		assertFileBytes(t, filepath.Join(snapshotDir, "manifest.json"), manifest)
		assertNotExist(t, filepath.Join(snapshotDir, "complete.json"))
	})
}

func TestPutSnapshotCompletesExistingIncompleteSnapshot(t *testing.T) {
	root := t.TempDir()
	name, oldManifest, _ := snapshotBytes(
		t,
		"tvdb:370070",
		7,
		false,
		false,
	)
	newName, newManifest, complete := snapshotBytes(
		t,
		"tvdb:370070",
		7,
		true,
		true,
	)
	if newName != name {
		t.Fatalf("snapshot names differ: old %q, new %q", name, newName)
	}
	if bytes.Equal(oldManifest, newManifest) {
		t.Fatal("snapshot manifests are identical; test requires different valid bytes")
	}
	mustSucceed(
		t,
		tapecatalog.PutSnapshot(
			root,
			firstVolumeID,
			name,
			oldManifest,
			nil,
		),
	)
	mustSucceed(
		t,
		tapecatalog.PutSnapshot(
			root,
			firstVolumeID,
			name,
			newManifest,
			complete,
		),
	)

	snapshotDir, err := tapevolume.SnapshotDir(
		activeVolumeDir(root, firstVolumeID),
		"tvdb:370070",
		7,
	)
	mustSucceed(t, err)
	assertFileBytes(t, filepath.Join(snapshotDir, "manifest.json"), newManifest)
	assertFileBytes(t, filepath.Join(snapshotDir, "complete.json"), complete)
	_, err = tapemanifest.Read(snapshotDir)
	mustSucceed(t, err)
}

func TestPutSnapshotRefusesReplacingIncompleteWithIncomplete(t *testing.T) {
	root := t.TempDir()
	name, oldManifest, _ := snapshotBytes(
		t,
		"tvdb:370070",
		7,
		false,
		false,
	)
	_, newManifest, _ := snapshotBytes(
		t,
		"tvdb:370070",
		7,
		false,
		true,
	)
	mustSucceed(
		t,
		tapecatalog.PutSnapshot(
			root,
			firstVolumeID,
			name,
			oldManifest,
			nil,
		),
	)
	snapshotDir, err := tapevolume.SnapshotDir(
		activeVolumeDir(root, firstVolumeID),
		"tvdb:370070",
		7,
	)
	mustSucceed(t, err)

	err = tapecatalog.PutSnapshot(
		root,
		firstVolumeID,
		name,
		newManifest,
		nil,
	)
	want := "tapecatalog: put snapshot " + name + ": destination " +
		strconv.Quote(snapshotDir) + " already exists"
	assertExactError(t, err, want)
	assertFileBytes(t, filepath.Join(snapshotDir, "manifest.json"), oldManifest)
	assertNotExist(t, filepath.Join(snapshotDir, "complete.json"))
}

func TestPutSnapshotRefusesReplacingCorruptCommittedSnapshot(t *testing.T) {
	root := t.TempDir()
	name, manifest, complete := snapshotBytes(
		t,
		"tvdb:370070",
		7,
		true,
		false,
	)
	mustSucceed(
		t,
		tapecatalog.PutSnapshot(
			root,
			firstVolumeID,
			name,
			manifest,
			complete,
		),
	)
	snapshotDir, err := tapevolume.SnapshotDir(
		activeVolumeDir(root, firstVolumeID),
		"tvdb:370070",
		7,
	)
	mustSucceed(t, err)
	manifestPath := filepath.Join(snapshotDir, "manifest.json")
	completePath := filepath.Join(snapshotDir, "complete.json")
	corruptManifest := append(append([]byte(nil), manifest...), ' ')
	mustSucceed(t, os.WriteFile(manifestPath, corruptManifest, 0o664))
	_, corruptionErr := tapemanifest.Read(snapshotDir)
	if corruptionErr == nil || errors.Is(corruptionErr, tapemanifest.ErrIncomplete) {
		t.Fatalf("corrupt committed snapshot error = %v, want non-incomplete corruption", corruptionErr)
	}

	err = tapecatalog.PutSnapshot(
		root,
		firstVolumeID,
		name,
		manifest,
		complete,
	)
	want := "tapecatalog: put snapshot " + name +
		": inspect destination: " + corruptionErr.Error()
	assertExactError(t, err, want)
	assertFileBytes(t, manifestPath, corruptManifest)
	assertFileBytes(t, completePath, complete)
}

func TestPutSnapshotRefusesDestinationLstatError(t *testing.T) {
	root := t.TempDir()
	mustSucceed(
		t,
		tapecatalog.SaveObserved(root, firstVolumeID, validObserved()),
	)
	name, manifest, complete := snapshotBytes(
		t,
		"tvdb:370070",
		7,
		true,
		false,
	)
	snapshotsDir := tapevolume.SnapshotsDir(
		activeVolumeDir(root, firstVolumeID),
	)
	destination := filepath.Join(snapshotsDir, name)
	writeMarker(t, destination, "unchanged")
	mustSucceed(t, os.Chmod(snapshotsDir, 0))
	t.Cleanup(func() {
		_ = os.Chmod(snapshotsDir, 0o775)
	})
	_, lstatErr := os.Lstat(destination)
	if !errors.Is(lstatErr, os.ErrPermission) {
		t.Fatalf("Lstat setup error = %v, want os.ErrPermission", lstatErr)
	}

	err := tapecatalog.PutSnapshot(
		root,
		firstVolumeID,
		name,
		manifest,
		complete,
	)
	want := "tapecatalog: put snapshot " + name +
		": inspect destination: " + lstatErr.Error()
	assertExactError(t, err, want)
	mustSucceed(t, os.Chmod(snapshotsDir, 0o775))
	assertMarker(t, destination, "unchanged")
}

func TestPutSnapshotRefusesReplacingCompleteSnapshot(t *testing.T) {
	root := t.TempDir()
	name, manifest, complete := snapshotBytes(
		t,
		"tvdb:370070",
		7,
		true,
		false,
	)
	replacementName, replacementManifest, replacementComplete := snapshotBytes(
		t,
		"tvdb:370070",
		7,
		true,
		true,
	)
	if replacementName != name {
		t.Fatalf("snapshot names differ: original %q, replacement %q", name, replacementName)
	}
	if bytes.Equal(manifest, replacementManifest) {
		t.Fatal("snapshot manifests are identical; test requires different valid bytes")
	}
	mustSucceed(
		t,
		tapecatalog.PutSnapshot(
			root,
			firstVolumeID,
			name,
			manifest,
			complete,
		),
	)
	snapshotDir, err := tapevolume.SnapshotDir(
		activeVolumeDir(root, firstVolumeID),
		"tvdb:370070",
		7,
	)
	mustSucceed(t, err)

	err = tapecatalog.PutSnapshot(
		root,
		firstVolumeID,
		name,
		replacementManifest,
		replacementComplete,
	)
	want := "tapecatalog: put snapshot " + name + ": destination " +
		strconv.Quote(snapshotDir) + " already exists"
	assertExactError(t, err, want)
	assertFileBytes(t, filepath.Join(snapshotDir, "manifest.json"), manifest)
	assertFileBytes(t, filepath.Join(snapshotDir, "complete.json"), complete)
}

func TestPutSnapshotRefusesDowngradingCompleteSnapshot(t *testing.T) {
	root := t.TempDir()
	name, manifest, complete := snapshotBytes(
		t,
		"tvdb:370070",
		7,
		true,
		false,
	)
	replacementName, replacementManifest, _ := snapshotBytes(
		t,
		"tvdb:370070",
		7,
		false,
		true,
	)
	if replacementName != name {
		t.Fatalf("snapshot names differ: original %q, replacement %q", name, replacementName)
	}
	if bytes.Equal(manifest, replacementManifest) {
		t.Fatal("snapshot manifests are identical; test requires different valid bytes")
	}
	mustSucceed(
		t,
		tapecatalog.PutSnapshot(
			root,
			firstVolumeID,
			name,
			manifest,
			complete,
		),
	)
	snapshotDir, err := tapevolume.SnapshotDir(
		activeVolumeDir(root, firstVolumeID),
		"tvdb:370070",
		7,
	)
	mustSucceed(t, err)

	err = tapecatalog.PutSnapshot(
		root,
		firstVolumeID,
		name,
		replacementManifest,
		nil,
	)
	want := "tapecatalog: put snapshot " + name + ": destination " +
		strconv.Quote(snapshotDir) + " already exists"
	assertExactError(t, err, want)
	assertFileBytes(t, filepath.Join(snapshotDir, "manifest.json"), manifest)
	assertFileBytes(t, filepath.Join(snapshotDir, "complete.json"), complete)
}

func TestMirroredVolumeUsesExistingReaders(t *testing.T) {
	root := t.TempDir()
	header := volumeHeaderBytes(t, firstVolumeID)
	name, manifestBytes, complete := snapshotBytes(
		t,
		"tvdb:370070",
		7,
		true,
		false,
	)
	mustSucceed(
		t,
		tapecatalog.PutVolumeHeader(root, firstVolumeID, header),
	)
	mustSucceed(
		t,
		tapecatalog.PutSnapshot(
			root,
			firstVolumeID,
			name,
			manifestBytes,
			complete,
		),
	)

	volumeDir, err := tapecatalog.VolumeDir(root, firstVolumeID)
	mustSucceed(t, err)
	if got, want := tapevolume.VolumeFile(volumeDir), filepath.Join(
		volumeDir,
		"KURA_ARCHIVE",
		"volume.json",
	); got != want {
		t.Fatalf("VolumeFile() = %q, want %q", got, want)
	}

	assertMirroredVolumeReadable(t, root, manifestBytes)
	mustSucceed(t, tapecatalog.Detach(root, firstVolumeID))
	assertMirroredVolumeReadable(t, root, manifestBytes)
	mustSucceed(t, tapecatalog.Attach(root, firstVolumeID))
	assertMirroredVolumeReadable(t, root, manifestBytes)
}

func assertMirroredVolumeReadable(t *testing.T, root string, manifestBytes []byte) {
	t.Helper()
	volumeDir, err := tapecatalog.VolumeDir(root, firstVolumeID)
	mustSucceed(t, err)
	gotVolume, err := tapevolume.Read(volumeDir)
	mustSucceed(t, err)
	if gotVolume.VolumeID != firstVolumeID {
		t.Fatalf("volumeID = %q, want %q", gotVolume.VolumeID, firstVolumeID)
	}
	snapshotDir, err := tapevolume.SnapshotDir(
		volumeDir,
		"tvdb:370070",
		7,
	)
	mustSucceed(t, err)
	gotManifest, err := tapemanifest.Read(snapshotDir)
	mustSucceed(t, err)
	if gotManifest.MetadataRef != "tvdb:370070" || gotManifest.Generation != 7 {
		t.Fatalf(
			"manifest identity = (%q, %d), want (%q, %d)",
			gotManifest.MetadataRef,
			gotManifest.Generation,
			"tvdb:370070",
			7,
		)
	}
	assertFileBytes(t, filepath.Join(snapshotDir, "manifest.json"), manifestBytes)
}

func TestVolumeDirLookup(t *testing.T) {
	t.Run("prefers active", func(t *testing.T) {
		root := t.TempDir()
		active := activeVolumeDir(root, firstVolumeID)
		detached := detachedVolumeDir(root, firstVolumeID)
		writeMarker(t, active, "active")
		writeMarker(t, detached, "detached")

		got, err := tapecatalog.VolumeDir(root, firstVolumeID)
		mustSucceed(t, err)
		if got != active {
			t.Fatalf("VolumeDir() = %q, want active %q", got, active)
		}
	})

	t.Run("finds detached", func(t *testing.T) {
		root := t.TempDir()
		detached := detachedVolumeDir(root, firstVolumeID)
		writeMarker(t, detached, "detached")

		got, err := tapecatalog.VolumeDir(root, firstVolumeID)
		mustSucceed(t, err)
		if got != detached {
			t.Fatalf("VolumeDir() = %q, want detached %q", got, detached)
		}
	})

	t.Run("reports absence", func(t *testing.T) {
		_, err := tapecatalog.VolumeDir(t.TempDir(), firstVolumeID)
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("VolumeDir error = %v, want os.ErrNotExist", err)
		}
		want := "tapecatalog: volume " + string(firstVolumeID) +
			" does not exist: file does not exist"
		assertExactError(t, err, want)
	})
}

func TestObservedValidationOnEncodeAndDecode(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*tapecatalog.Observed)
		want   string
	}{
		{
			name:   "tape ID required",
			mutate: func(observed *tapecatalog.Observed) { observed.TapeID = "" },
			want:   "tapecatalog: tapeID is required",
		},
		{
			name: "tape ID barcode validated",
			mutate: func(observed *tapecatalog.Observed) {
				observed.TapeID = "ABC123CU"
			},
			want: `tapecatalog: tapeID "ABC123CU" identifies a cleaning cartridge`,
		},
		{
			name: "observed time required",
			mutate: func(observed *tapecatalog.Observed) {
				observed.ObservedAt = time.Time{}
			},
			want: "tapecatalog: observedAt is required",
		},
		{
			name: "observed time UTC",
			mutate: func(observed *tapecatalog.Observed) {
				observed.ObservedAt = observed.ObservedAt.In(
					time.FixedZone("offset", 3600),
				)
			},
			want: "tapecatalog: observedAt must be UTC",
		},
		{
			name: "capacity nonnegative",
			mutate: func(observed *tapecatalog.Observed) {
				observed.CapacityBytes = -1
			},
			want: "tapecatalog: capacityBytes must not be negative",
		},
		{
			name: "free nonnegative",
			mutate: func(observed *tapecatalog.Observed) {
				observed.FreeBytes = -1
			},
			want: "tapecatalog: freeBytes must not be negative",
		},
		{
			name: "free within capacity",
			mutate: func(observed *tapecatalog.Observed) {
				observed.FreeBytes = observed.CapacityBytes + 1
			},
			want: "tapecatalog: freeBytes must not exceed capacityBytes",
		},
		{
			name: "last verified time UTC",
			mutate: func(observed *tapecatalog.Observed) {
				observed.LastVerifiedAt = observed.LastVerifiedAt.In(
					time.FixedZone("offset", 3600),
				)
			},
			want: "tapecatalog: lastVerifiedAt must be UTC",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Run("encode", func(t *testing.T) {
				root := t.TempDir()
				observed := validObserved()
				test.mutate(&observed)
				assertExactError(
					t,
					tapecatalog.SaveObserved(root, firstVolumeID, observed),
					test.want,
				)
				assertNotExist(t, activeVolumeDir(root, firstVolumeID))
			})

			t.Run("decode", func(t *testing.T) {
				root := t.TempDir()
				observed := validObserved()
				test.mutate(&observed)
				writeObservedWire(
					t,
					activeVolumeDir(root, firstVolumeID),
					observed,
				)
				_, err := tapecatalog.LoadObserved(root, firstVolumeID)
				assertExactError(t, err, test.want)
			})
		})
	}
}

func TestObservedRoundTripWire(t *testing.T) {
	root := t.TempDir()
	want := validObserved()
	mustSucceed(
		t,
		tapecatalog.SaveObserved(root, firstVolumeID, want),
	)
	got, err := tapecatalog.LoadObserved(root, firstVolumeID)
	mustSucceed(t, err)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadObserved() = %#v, want %#v", got, want)
	}

	data, err := os.ReadFile(
		filepath.Join(activeVolumeDir(root, firstVolumeID), "observed.json"),
	)
	mustSucceed(t, err)
	const wantJSON = `{
  "schemaVersion": 1,
  "tapeID": "ABC123L6",
  "observedAt": "2026-07-24T18:03:11Z",
  "capacityBytes": 2500000000000,
  "freeBytes": 812000000000,
  "lastVerifiedAt": "2026-07-22T09:00:00Z"
}
`
	if !bytes.Equal(data, []byte(wantJSON)) {
		t.Fatalf("observed.json =\n%s\nwant:\n%s", data, wantJSON)
	}
}

func TestLoadObservedRejectsUnsupportedSchemaVersion(t *testing.T) {
	root := t.TempDir()
	mustSucceed(
		t,
		tapecatalog.SaveObserved(root, firstVolumeID, validObserved()),
	)
	path := filepath.Join(activeVolumeDir(root, firstVolumeID), "observed.json")
	original, err := os.ReadFile(path)
	mustSucceed(t, err)
	future := bytes.Replace(
		original,
		[]byte(`"schemaVersion": 1`),
		[]byte(`"schemaVersion": 2`),
		1,
	)
	if bytes.Equal(future, original) {
		t.Fatal("schemaVersion mutation did not change observed.json")
	}
	mustSucceed(t, os.WriteFile(path, future, 0o664))

	_, err = tapecatalog.LoadObserved(root, firstVolumeID)
	assertExactError(t, err, "tapecatalog: unsupported observed schemaVersion 2")
	assertFileBytes(t, path, future)
}

func TestPutVolumeHeaderStoresBytesAndChecksIdentity(t *testing.T) {
	root := t.TempDir()
	header := append(volumeHeaderBytes(t, firstVolumeID), ' ')
	mustSucceed(
		t,
		tapecatalog.PutVolumeHeader(root, firstVolumeID, header),
	)
	assertFileBytes(
		t,
		tapevolume.VolumeFile(activeVolumeDir(root, firstVolumeID)),
		header,
	)

	otherHeader := volumeHeaderBytes(t, secondVolumeID)
	want := `tapecatalog: volumeID mismatch: catalog is "` +
		string(firstVolumeID) + `", header contains "` + string(secondVolumeID) + `"`
	assertExactError(
		t,
		tapecatalog.PutVolumeHeader(root, firstVolumeID, otherHeader),
		want,
	)
}

func TestListVolumeDirectories(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, activeVolumeDir(root, secondVolumeID), "second")
	writeMarker(t, activeVolumeDir(root, firstVolumeID), "first")
	writeMarker(t, detachedVolumeDir(root, thirdVolumeID), "third")
	writeMarker(
		t,
		filepath.Join(paths.ActiveVolumeCatalogDir(root), ".volume-temp"),
		"temp",
	)
	mustSucceed(
		t,
		os.WriteFile(
			filepath.Join(paths.ActiveVolumeCatalogDir(root), string(thirdVolumeID)),
			[]byte("not a directory"),
			0o644,
		),
	)

	active, err := tapecatalog.ListActive(root)
	mustSucceed(t, err)
	wantActive := []volume.ID{firstVolumeID, secondVolumeID}
	if !reflect.DeepEqual(active, wantActive) {
		t.Fatalf("ListActive() = %v, want %v", active, wantActive)
	}
	detached, err := tapecatalog.ListDetached(root)
	mustSucceed(t, err)
	if !reflect.DeepEqual(detached, []volume.ID{thirdVolumeID}) {
		t.Fatalf("ListDetached() = %v, want [%s]", detached, thirdVolumeID)
	}
}

func TestListMissingDirectories(t *testing.T) {
	root := t.TempDir()
	active, err := tapecatalog.ListActive(root)
	mustSucceed(t, err)
	if !reflect.DeepEqual(active, []volume.ID{}) {
		t.Fatalf("ListActive() = %#v, want empty non-nil slice", active)
	}
	detached, err := tapecatalog.ListDetached(root)
	mustSucceed(t, err)
	if !reflect.DeepEqual(detached, []volume.ID{}) {
		t.Fatalf("ListDetached() = %#v, want empty non-nil slice", detached)
	}
}

func TestPathUnsafeVolumeIDsRejected(t *testing.T) {
	ids := []volume.ID{"", "../escape", "01j8zq7w5twha6r6j8x4qz9y7v"}
	for _, id := range ids {
		t.Run(string(id), func(t *testing.T) {
			root := t.TempDir()
			want := invalidVolumeIDError(id)
			_, err := tapecatalog.VolumeDir(root, id)
			assertExactError(t, err, want)
			assertExactError(
				t,
				tapecatalog.SaveObserved(root, id, validObserved()),
				want,
			)
			_, err = tapecatalog.LoadObserved(root, id)
			assertExactError(t, err, want)
			assertExactError(t, tapecatalog.PutVolumeHeader(root, id, nil), want)
			assertExactError(
				t,
				tapecatalog.PutSnapshot(root, id, "snapshot", nil, nil),
				want,
			)
			assertExactError(t, tapecatalog.Detach(root, id), want)
			assertExactError(t, tapecatalog.Attach(root, id), want)
			assertExactError(t, tapecatalog.Purge(root, id), want)
		})
	}
}

func TestPutSnapshotRejectsUnsafeName(t *testing.T) {
	err := tapecatalog.PutSnapshot(
		t.TempDir(),
		firstVolumeID,
		"../escape",
		nil,
		nil,
	)
	const want = `tapecatalog: put snapshot ../escape: ` +
		`tapevolume: snapshot name "../escape" is missing .g<generation>`
	assertExactError(t, err, want)
}

func validObserved() tapecatalog.Observed {
	return tapecatalog.Observed{
		TapeID:         "ABC123L6",
		ObservedAt:     time.Date(2026, 7, 24, 18, 3, 11, 0, time.UTC),
		CapacityBytes:  2_500_000_000_000,
		FreeBytes:      812_000_000_000,
		LastVerifiedAt: time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC),
	}
}

func volumeHeaderBytes(t *testing.T, id volume.ID) []byte {
	t.Helper()
	root := t.TempDir()
	mustSucceed(
		t,
		tapevolume.Write(root, tapevolume.Volume{
			VolumeID:  id,
			TapeID:    "ABC123L6",
			CreatedAt: time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
		}),
	)
	data, err := os.ReadFile(tapevolume.VolumeFile(root))
	mustSucceed(t, err)
	return data
}

func snapshotBytes(
	t *testing.T,
	metadataRef string,
	generation int,
	complete, addWhitespace bool,
) (name string, manifestData, completeData []byte) {
	t.Helper()
	root := t.TempDir()
	name, err := tapevolume.SnapshotName(metadataRef, generation)
	mustSucceed(t, err)
	snapshotDir, err := tapevolume.SnapshotDir(root, metadataRef, generation)
	mustSucceed(t, err)
	manifest := tapemanifest.Manifest{
		MetadataRef: metadataRef,
		Title:       "Show",
		Generation:  generation,
		CapturedAt:  time.Date(2026, 7, 21, 13, 10, 0, 0, time.UTC),
		WrittenBy: tapemanifest.Writer{
			Version: "v0.1.0",
			Host:    "tape-vm",
		},
		TotalBytes: 15,
		Files: []tapemanifest.File{
			{
				Path:    "Season 1/Show - S01E01.mkv",
				Size:    15,
				ModTime: time.Date(2026, 7, 21, 13, 0, 0, 0, time.UTC),
				Hash: "sha256:" +
					strings.Repeat("a", 64),
			},
		},
	}
	mustSucceed(t, tapemanifest.Write(snapshotDir, manifest))
	manifestPath := filepath.Join(snapshotDir, "manifest.json")
	manifestData, err = os.ReadFile(manifestPath)
	mustSucceed(t, err)
	if addWhitespace {
		manifestData = append(manifestData, ' ', '\n')
		mustSucceed(t, os.WriteFile(manifestPath, manifestData, 0o644))
	}
	if !complete {
		return name, manifestData, nil
	}
	mustSucceed(t, tapemanifest.Commit(snapshotDir))
	completeData, err = os.ReadFile(filepath.Join(snapshotDir, "complete.json"))
	mustSucceed(t, err)
	return name, manifestData, completeData
}

func writeObservedWire(
	t *testing.T,
	volumeDir string,
	observed tapecatalog.Observed,
) {
	t.Helper()
	wire := map[string]any{
		"schemaVersion":  1,
		"tapeID":         string(observed.TapeID),
		"observedAt":     formatTime(observed.ObservedAt),
		"capacityBytes":  observed.CapacityBytes,
		"freeBytes":      observed.FreeBytes,
		"lastVerifiedAt": formatTime(observed.LastVerifiedAt),
	}
	data, err := json.MarshalIndent(wire, "", "  ")
	mustSucceed(t, err)
	mustSucceed(t, os.MkdirAll(volumeDir, 0o755))
	mustSucceed(
		t,
		os.WriteFile(
			filepath.Join(volumeDir, "observed.json"),
			append(data, '\n'),
			0o644,
		),
	)
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func activeVolumeDir(root string, id volume.ID) string {
	return filepath.Join(paths.ActiveVolumeCatalogDir(root), string(id))
}

func detachedVolumeDir(root string, id volume.ID) string {
	return filepath.Join(paths.DetachedVolumeCatalogDir(root), string(id))
}

func writeMarker(t *testing.T, dir, value string) {
	t.Helper()
	mustSucceed(t, os.MkdirAll(dir, 0o755))
	mustSucceed(
		t,
		os.WriteFile(filepath.Join(dir, "marker"), []byte(value), 0o644),
	)
}

func assertMarker(t *testing.T, dir, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, "marker"))
	mustSucceed(t, err)
	if string(got) != want {
		t.Fatalf("marker at %q = %q, want %q", dir, got, want)
	}
}

func assertDisjoint(t *testing.T, root string, id volume.ID) {
	t.Helper()
	active := pathExists(t, activeVolumeDir(root, id))
	detached := pathExists(t, detachedVolumeDir(root, id))
	if active && detached {
		t.Fatalf(
			"volume %s exists in both %q and %q",
			id,
			activeVolumeDir(root, id),
			detachedVolumeDir(root, id),
		)
	}
}

func pathExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	t.Fatalf("Lstat %q: %v", path, err)
	return false
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	_, err := os.Lstat(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%q exists or has unexpected error: %v", path, err)
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	mustSucceed(t, err)
	if !bytes.Equal(got, want) {
		t.Fatalf("file %q = %q, want byte-identical %q", path, got, want)
	}
}

func assertExactError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func mustSucceed(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func invalidVolumeIDError(id volume.ID) string {
	if id == "" {
		return "tapecatalog: volumeID is required"
	}
	return "tapecatalog: volumeID " + strconv.Quote(string(id)) +
		" must be a 26-character uppercase Crockford base32 ULID"
}

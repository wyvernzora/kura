package executor_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/executor"
	"github.com/wyvernzora/kura/services/tape-backup/internal/fingerprint"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapecatalog"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapemanifest"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"golang.org/x/text/unicode/norm"
)

func TestCopyRejectsEachPostCopyDrift(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*testing.T, copyFixture)
		wantReason backupplan.ItemReason
	}{
		{
			name: "live file added",
			mutate: func(t *testing.T, fixture copyFixture) {
				writeFile(
					t,
					filepath.Join(fixture.seriesRoot, "late-added.ass"),
					[]byte("late"),
				)
			},
			wantReason: backupplan.ReasonPayloadDrift,
		},
		{
			name: "series root removed",
			mutate: func(t *testing.T, fixture copyFixture) {
				if err := os.RemoveAll(fixture.seriesRoot); err != nil {
					t.Fatalf("RemoveAll series root: %v", err)
				}
			},
			wantReason: backupplan.ReasonSeriesRootMissing,
		},
		{
			name: "generation changed",
			mutate: func(t *testing.T, fixture copyFixture) {
				writeSeriesMetadata(t, fixture.seriesRoot, seriesMetadata{
					Generation:  fixture.action.Generation + 1,
					MetadataRef: fixture.action.MetadataRef,
				})
			},
			wantReason: backupplan.ReasonSeriesMoved,
		},
		{
			name: "metadata ref changed",
			mutate: func(t *testing.T, fixture copyFixture) {
				writeSeriesMetadata(t, fixture.seriesRoot, seriesMetadata{
					Generation:  fixture.action.Generation,
					MetadataRef: "tvdb:different",
				})
			},
			wantReason: backupplan.ReasonSeriesMoved,
		},
		{
			name: "staging created",
			mutate: func(t *testing.T, fixture copyFixture) {
				writeSeriesMetadata(t, fixture.seriesRoot, seriesMetadata{
					Generation:    fixture.action.Generation,
					MetadataRef:   fixture.action.MetadataRef,
					EpisodeStaged: true,
				})
			},
			wantReason: backupplan.ReasonStagedIntent,
		},
		{
			name: "claim created",
			mutate: func(t *testing.T, fixture copyFixture) {
				writeSeriesMetadata(t, fixture.seriesRoot, seriesMetadata{
					Generation:  fixture.action.Generation,
					MetadataRef: fixture.action.MetadataRef,
					ActiveClaim: true,
				})
			},
			wantReason: backupplan.ReasonClaimStale,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCopyFixture(t, nil)
			var once sync.Once
			handler := newCopyHandler(t, nil, func(path string) (executor.Destination, error) {
				if strings.HasSuffix(path, "episode.mkv") {
					once.Do(func() { test.mutate(t, fixture) })
				}
				return executor.OpenDestination(path)
			})

			execution, err := executor.ExecutePreparedPlan(
				t.Context(),
				fixture.prepared,
				0,
				handler,
			)
			if err != nil {
				t.Fatalf("ExecutePreparedPlan: %v", err)
			}
			closeOutbox(t, fixture.outbox)
			if len(execution.Written) != 0 {
				t.Fatalf("written results = %d, want 0", len(execution.Written))
			}
			assertItemFailureReason(
				t,
				fixture.sink.Events(),
				fixture.action,
				test.wantReason,
			)
			assertSnapshotAbsent(t, fixture)
		})
	}
}

func TestCopyRejectsNonNFCEntryBeforeOpeningAnyDestination(t *testing.T) {
	fixture := newCopyFixture(t, nil)
	nfdPath := "Cafe\u0301.ass"
	if norm.NFC.IsNormalString(nfdPath) {
		t.Fatalf("test path %q is NFC-normalized", nfdPath)
	}
	writeFile(t, filepath.Join(fixture.seriesRoot, nfdPath), []byte("subtitle"))
	fixture.refreshAction(t)
	destinationsOpened := 0
	handler := newCopyHandler(t, nil, func(path string) (executor.Destination, error) {
		destinationsOpened++
		return executor.OpenDestination(path)
	})

	execution, err := executor.ExecutePreparedPlan(
		t.Context(),
		fixture.prepared,
		0,
		handler,
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, fixture.outbox)
	if destinationsOpened != 0 {
		t.Fatalf("destinations opened = %d, want 0", destinationsOpened)
	}
	if len(execution.Written) != 0 {
		t.Fatalf("written results = %d, want 0", len(execution.Written))
	}
	assertEvent(
		t,
		fixture.sink.Events(),
		backupplan.EventItemFailed,
		func(event backupplan.Event) bool {
			return event.MetadataRef == fixture.action.MetadataRef &&
				event.Generation == fixture.action.Generation &&
				event.Reason == string(backupplan.ReasonUnsupportedFileType) &&
				strings.Contains(event.Detail, nfdPath)
		},
	)
	assertSnapshotAbsent(t, fixture)
}

func TestCopyDurabilityBarrierRejectsSyncFailure(t *testing.T) {
	fixture := newCopyFixture(t, nil)
	syncFailure := errors.New("injected destination sync failure")
	handler := newCopyHandler(t, nil, func(path string) (executor.Destination, error) {
		destination, err := openWrappedDestination(path)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(path, "episode.mkv") {
			destination.syncErr = syncFailure
		}
		return destination, nil
	})

	execution, err := executor.ExecutePreparedPlan(
		t.Context(),
		fixture.prepared,
		0,
		handler,
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, fixture.outbox)
	if len(execution.Written) != 0 {
		t.Fatalf("written results = %d, want 0", len(execution.Written))
	}
	assertItemFailureDetail(t, fixture, syncFailure.Error())
	assertSnapshotAbsent(t, fixture)
}

func TestCopyDurabilityBarrierRejectsCloseFailure(t *testing.T) {
	fixture := newCopyFixture(t, nil)
	closeFailure := errors.New("injected destination close failure")
	handler := newCopyHandler(t, nil, func(path string) (executor.Destination, error) {
		destination, err := openWrappedDestination(path)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(path, "episode.mkv") {
			destination.closeErr = closeFailure
		}
		return destination, nil
	})

	execution, err := executor.ExecutePreparedPlan(
		t.Context(),
		fixture.prepared,
		0,
		handler,
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, fixture.outbox)
	if len(execution.Written) != 0 {
		t.Fatalf("written results = %d, want 0", len(execution.Written))
	}
	assertItemFailureDetail(t, fixture, closeFailure.Error())
	assertSnapshotAbsent(t, fixture)
}

func TestCopyDurabilityBarrierRejectsByteCountMismatch(t *testing.T) {
	fixture := newCopyFixture(t, bytes.Repeat([]byte("payload"), 8192))
	sourcePath := filepath.Join(fixture.seriesRoot, "episode.mkv")
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile source: %v", err)
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatalf("Stat source: %v", err)
	}
	handler := newCopyHandler(t, nil, func(path string) (executor.Destination, error) {
		destination, err := openWrappedDestination(path)
		if err != nil {
			return nil, err
		}
		if !strings.HasSuffix(path, "episode.mkv") {
			return destination, nil
		}
		if err := os.Truncate(sourcePath, int64(len(sourceBytes)/2)); err != nil {
			t.Fatalf("Truncate source: %v", err)
		}
		destination.beforeClose = func() error {
			if err := os.WriteFile(sourcePath, sourceBytes, 0o664); err != nil {
				return err
			}
			return os.Chtimes(sourcePath, sourceInfo.ModTime(), sourceInfo.ModTime())
		}
		return destination, nil
	})

	execution, err := executor.ExecutePreparedPlan(
		t.Context(),
		fixture.prepared,
		0,
		handler,
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, fixture.outbox)
	if len(execution.Written) != 0 {
		t.Fatalf("written results = %d, want 0", len(execution.Written))
	}
	assertItemFailureDetail(t, fixture, "want source size")
	assertSnapshotAbsent(t, fixture)
}

func TestCopyManifestMatchesDestinationAndPreservesCanonicalOrder(t *testing.T) {
	fixture := newCopyFixture(t, []byte("episode payload"))
	writeFile(t, filepath.Join(fixture.seriesRoot, "a.ass"), []byte("subtitle"))
	writeFile(
		t,
		filepath.Join(fixture.seriesRoot, ".kura", "trash", "ignored.mkv"),
		[]byte("ignored"),
	)
	fixture.refreshAction(t)
	handler := newCopyHandler(t, nil, executor.OpenDestination)

	execution, err := executor.ExecutePreparedPlan(
		t.Context(),
		fixture.prepared,
		0,
		handler,
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, fixture.outbox)
	if len(execution.Written) != 1 {
		t.Fatalf("written results = %d, want 1", len(execution.Written))
	}
	snapshotDir := fixture.snapshotDir(t)
	manifest, err := tapemanifest.Read(snapshotDir)
	if err != nil {
		t.Fatalf("Read committed manifest: %v", err)
	}
	paths := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		paths = append(paths, file.Path)
		data, err := os.ReadFile(filepath.Join(
			snapshotDir,
			"tree",
			filepath.FromSlash(file.Path),
		))
		if err != nil {
			t.Fatalf("ReadFile destination %q: %v", file.Path, err)
		}
		sum := sha256.Sum256(data)
		wantHash := "sha256:" + hex.EncodeToString(sum[:])
		if file.Hash != wantHash {
			t.Fatalf("hash for %q = %q, want %q", file.Path, file.Hash, wantHash)
		}
	}
	wantPaths := []string{".kura/series.json", "a.ass", "episode.mkv"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("manifest paths = %#v, want %#v", paths, wantPaths)
	}
	assertEvent(
		t,
		fixture.sink.Events(),
		backupplan.EventFileStarted,
		func(event backupplan.Event) bool {
			return event.Path == ".kura/series.json"
		},
	)
}

func TestCopyCancellationAbortsMidFileAndRemovesSnapshot(t *testing.T) {
	fixture := newCopyFixture(t, bytes.Repeat([]byte("x"), 1<<20))
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	handler := newCopyHandler(t, nil, func(path string) (executor.Destination, error) {
		destination, err := openWrappedDestination(path)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(path, "episode.mkv") {
			destination.write = func(data []byte) (int, error) {
				select {
				case <-started:
				default:
					close(started)
				}
				<-ctx.Done()
				return 0, ctx.Err()
			}
		}
		return destination, nil
	})
	result := make(chan error, 1)
	go func() {
		_, err := executor.ExecutePreparedPlan(ctx, fixture.prepared, 0, handler)
		result <- err
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ExecutePreparedPlan error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("copy did not abort within one second of cancellation")
	}
	closeOutbox(t, fixture.outbox)
	assertSnapshotAbsent(t, fixture)
}

func TestCopyProgressDropsOldestWithoutStalling(t *testing.T) {
	progress, err := executor.NewFileProgressChannel(1)
	if err != nil {
		t.Fatalf("NewFileProgressChannel: %v", err)
	}
	fixture := newCopyFixture(t, bytes.Repeat([]byte("p"), 1<<20))
	handler := newCopyHandler(t, progress, func(path string) (executor.Destination, error) {
		return openWrappedDestination(path)
	})
	done := make(chan error, 1)
	go func() {
		_, err := executor.ExecutePreparedPlan(
			t.Context(),
			fixture.prepared,
			0,
			handler,
		)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExecutePreparedPlan: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stalled progress consumer blocked the copy")
	}
	closeOutbox(t, fixture.outbox)
	select {
	case frame := <-progress.Frames():
		if frame.Path != "episode.mkv" {
			t.Fatalf("latest progress path = %q, want episode.mkv", frame.Path)
		}
		if frame.FileBytesWritten != frame.FileBytesTotal {
			t.Fatalf(
				"latest progress bytes = %d/%d, want completed file",
				frame.FileBytesWritten,
				frame.FileBytesTotal,
			)
		}
	default:
		t.Fatal("progress channel is empty")
	}
}

func TestFileProgressChannelHidesWritableChannel(t *testing.T) {
	progress, err := executor.NewFileProgressChannel(1)
	if err != nil {
		t.Fatalf("NewFileProgressChannel: %v", err)
	}
	progressType := reflect.TypeOf(progress)
	if progressType.Kind() != reflect.Pointer ||
		progressType.Elem().Kind() != reflect.Struct {
		t.Fatalf("progress type = %v, want pointer to struct", progressType)
	}
	for i := range progressType.Elem().NumField() {
		field := progressType.Elem().Field(i)
		if field.Type.Kind() == reflect.Chan && field.IsExported() {
			t.Fatalf("progress channel field %q is exported", field.Name)
		}
	}
	if direction := reflect.TypeOf(progress.Frames()).ChanDir(); direction != reflect.RecvDir {
		t.Fatalf("Frames direction = %v, want receive-only", direction)
	}
}

func TestCopyRecordsCapacityThroughDeclaredSeam(t *testing.T) {
	fixture := newCopyFixture(t, nil)
	recorder := &recordingCapacityRecorder{}
	handler, err := executor.NewBackupActionHandler(
		nil,
		executor.OpenDestination,
		recorder,
	)
	if err != nil {
		t.Fatalf("NewBackupActionHandler: %v", err)
	}

	execution, err := executor.ExecutePreparedPlan(
		t.Context(),
		fixture.prepared,
		0,
		handler,
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, fixture.outbox)
	if len(execution.Written) != 1 {
		t.Fatalf("written results = %d, want 1", len(execution.Written))
	}
	if recorder.tapeID != fixture.prepared.Cartridge.TapeID {
		t.Fatalf(
			"recorded tapeID = %q, want %q",
			recorder.tapeID,
			fixture.prepared.Cartridge.TapeID,
		)
	}
	if recorder.bytes != fixture.action.Bytes {
		t.Fatalf("recorded bytes = %d, want %d", recorder.bytes, fixture.action.Bytes)
	}
}

func TestCopyRemovesUncommittedTargetBeforeWriting(t *testing.T) {
	fixture := newCopyFixture(t, nil)
	snapshotDir := fixture.snapshotDir(t)
	stalePath := filepath.Join(snapshotDir, "tree", "stale.mkv")
	writeFile(t, stalePath, []byte("stale"))
	handler := newCopyHandler(t, nil, executor.OpenDestination)

	execution, err := executor.ExecutePreparedPlan(
		t.Context(),
		fixture.prepared,
		0,
		handler,
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, fixture.outbox)
	if len(execution.Written) != 1 {
		t.Fatalf("written results = %d, want 1", len(execution.Written))
	}
	if _, err := os.Lstat(stalePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale remnant Lstat error = %v, want os.ErrNotExist", err)
	}
}

func TestCopyRefusesCommittedTargetReachingCopy(t *testing.T) {
	fixture := newCopyFixture(t, nil)
	_, _, completeBefore := writeCommittedSnapshot(
		t,
		fixture.prepared.Cartridge.Root,
		fixture.action,
	)
	handler := newCopyHandler(t, nil, executor.OpenDestination)

	execution, err := executor.ExecutePreparedPlan(
		t.Context(),
		fixture.prepared,
		0,
		handler,
	)
	if !errors.Is(err, executor.ErrPlanFailed) {
		t.Fatalf("ExecutePreparedPlan error = %v, want executor.ErrPlanFailed", err)
	}
	closeOutbox(t, fixture.outbox)
	if !reflect.DeepEqual(execution, executor.PreparedExecution{}) {
		t.Fatalf("execution = %#v, want zero result", execution)
	}
	completeAfter, err := os.ReadFile(filepath.Join(
		fixture.snapshotDir(t),
		"complete.json",
	))
	if err != nil {
		t.Fatalf("ReadFile complete after refusal: %v", err)
	}
	if !bytes.Equal(completeAfter, completeBefore) {
		t.Fatal("committed target completion marker changed")
	}
}

func TestResultsReleaseOnlyAfterDriveSyncAndCarryObservedFacts(t *testing.T) {
	syncDrive := &blockingSyncDrive{
		fixedCapacityDrive: fixedCapacityDrive{total: 4096, free: 3072},
		started:            make(chan struct{}),
		release:            make(chan struct{}),
	}
	fixture := newCopyFixture(t, nil)
	fixture.prepared.Drive = syncDrive
	handler := newCopyHandler(t, nil, executor.OpenDestination)
	results := make(chan executionOutcome, 1)
	go func() {
		execution, err := executor.ExecutePreparedPlan(
			t.Context(),
			fixture.prepared,
			0,
			handler,
		)
		results <- executionOutcome{execution: execution, err: err}
	}()
	select {
	case <-syncDrive.started:
	case <-time.After(time.Second):
		t.Fatal("Drive.Sync was not called")
	}
	select {
	case outcome := <-results:
		t.Fatalf("result released before Sync returned: %#v", outcome)
	default:
	}
	close(syncDrive.release)
	outcome := <-results
	if outcome.err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", outcome.err)
	}
	closeOutbox(t, fixture.outbox)
	if outcome.execution.TapeID != fixture.prepared.Cartridge.TapeID {
		t.Fatalf(
			"tapeID = %q, want %q",
			outcome.execution.TapeID,
			fixture.prepared.Cartridge.TapeID,
		)
	}
	if outcome.execution.ObservedAt.IsZero() {
		t.Fatal("observedAt is zero")
	}
	if outcome.execution.CapacityBytes != 4096 ||
		outcome.execution.FreeBytes != 3072 {
		t.Fatalf(
			"capacity/free = %d/%d, want 4096/3072",
			outcome.execution.CapacityBytes,
			outcome.execution.FreeBytes,
		)
	}
}

func TestFailingDriveSyncReleasesNoResults(t *testing.T) {
	syncFailure := errors.New("injected drive sync failure")
	drive := &fixedCapacityDrive{
		total:   4096,
		free:    3072,
		syncErr: syncFailure,
	}
	fixture := newCopyFixture(t, nil)
	fixture.prepared.Drive = drive
	handler := newCopyHandler(t, nil, executor.OpenDestination)

	execution, err := executor.ExecutePreparedPlan(
		t.Context(),
		fixture.prepared,
		0,
		handler,
	)
	if !errors.Is(err, syncFailure) {
		t.Fatalf("ExecutePreparedPlan error = %v, want sync failure", err)
	}
	closeOutbox(t, fixture.outbox)
	if !reflect.DeepEqual(execution, executor.PreparedExecution{}) {
		t.Fatalf("execution = %#v, want zero result", execution)
	}
}

func TestExecutorResultRoundTripsThroughTapeCatalogVerbatim(t *testing.T) {
	fixture := newCopyFixture(t, nil)
	handler := newCopyHandler(t, nil, executor.OpenDestination)
	execution, err := executor.ExecutePreparedPlan(
		t.Context(),
		fixture.prepared,
		0,
		handler,
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, fixture.outbox)
	if len(execution.Written) != 1 {
		t.Fatalf("written results = %d, want 1", len(execution.Written))
	}

	stateRoot := t.TempDir()
	result := execution.Written[0]
	name, err := tapevolume.SnapshotName(result.MetadataRef, result.Generation)
	if err != nil {
		t.Fatalf("SnapshotName: %v", err)
	}
	cartridgeManifest, err := os.ReadFile(filepath.Join(
		fixture.snapshotDir(t),
		"manifest.json",
	))
	if err != nil {
		t.Fatalf("ReadFile cartridge manifest: %v", err)
	}
	cartridgeComplete, err := os.ReadFile(filepath.Join(
		fixture.snapshotDir(t),
		"complete.json",
	))
	if err != nil {
		t.Fatalf("ReadFile cartridge complete: %v", err)
	}
	if !bytes.Equal(result.Manifest, cartridgeManifest) {
		t.Fatal("executor manifest result differs from cartridge bytes")
	}
	if !bytes.Equal(result.Complete, cartridgeComplete) {
		t.Fatal("executor completion result differs from cartridge bytes")
	}
	if err := tapecatalog.PutSnapshot(
		stateRoot,
		firstVolume,
		name,
		result.Manifest,
		result.Complete,
	); err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}
	volumeDir, err := tapecatalog.VolumeDir(stateRoot, firstVolume)
	if err != nil {
		t.Fatalf("VolumeDir: %v", err)
	}
	mirrorManifest, err := os.ReadFile(filepath.Join(
		tapevolume.SnapshotsDir(volumeDir),
		name,
		"manifest.json",
	))
	if err != nil {
		t.Fatalf("ReadFile mirror manifest: %v", err)
	}
	mirrorComplete, err := os.ReadFile(filepath.Join(
		tapevolume.SnapshotsDir(volumeDir),
		name,
		"complete.json",
	))
	if err != nil {
		t.Fatalf("ReadFile mirror complete: %v", err)
	}
	if !bytes.Equal(mirrorManifest, result.Manifest) {
		t.Fatal("mirror manifest bytes differ from executor result")
	}
	if !bytes.Equal(mirrorComplete, result.Complete) {
		t.Fatal("mirror completion bytes differ from executor result")
	}
}

type copyFixture struct {
	prepared   executor.PreparedPlan
	action     backupplan.Action
	seriesRoot string
	sink       *collectingSink
	outbox     *executor.EventOutbox
}

func newCopyFixture(t *testing.T, episodeBytes []byte) copyFixture {
	t.Helper()
	if episodeBytes == nil {
		episodeBytes = []byte("episode payload")
	}
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	const rootPath = "Anime Series"
	const metadataRef = "tvdb:370070"
	seriesRoot := filepath.Join(libraryRoot, rootPath)
	writeFile(t, filepath.Join(seriesRoot, "episode.mkv"), episodeBytes)
	writeSeriesMetadata(t, seriesRoot, seriesMetadata{
		Generation:  1,
		MetadataRef: metadataRef,
	})
	digest, err := fingerprint.ComputeExcludingKura(seriesRoot)
	if err != nil {
		t.Fatalf("ComputeExcludingKura: %v", err)
	}
	action := backupAction(metadataRef, rootPath, 1, string(digest), archiveBytes(t, seriesRoot))
	drive := &fixedCapacityDrive{total: 1 << 30, free: 1 << 30}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		libraryRoot,
		map[string]struct{}{},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			action,
		},
	)
	prepared.Plan.CreatedBy = backupplan.Creator{
		Version: "v0.6.0",
		Host:    "tape-vm",
	}
	return copyFixture{
		prepared:   prepared,
		action:     action,
		seriesRoot: seriesRoot,
		sink:       sink,
		outbox:     outbox,
	}
}

func (f *copyFixture) refreshAction(t *testing.T) {
	t.Helper()
	digest, err := fingerprint.ComputeExcludingKura(f.seriesRoot)
	if err != nil {
		t.Fatalf("ComputeExcludingKura: %v", err)
	}
	f.action.PayloadFingerprint = string(digest)
	f.action.Bytes = archiveBytes(t, f.seriesRoot)
	f.prepared.Plan.Actions[1] = f.action
}

func (f copyFixture) snapshotDir(t *testing.T) string {
	t.Helper()
	path, err := tapevolume.SnapshotDir(
		f.prepared.Cartridge.Root,
		f.action.MetadataRef,
		f.action.Generation,
	)
	if err != nil {
		t.Fatalf("SnapshotDir: %v", err)
	}
	return path
}

func archiveBytes(t *testing.T, seriesRoot string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(seriesRoot, func(
		path string,
		entry os.DirEntry,
		walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(seriesRoot, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if relative != "." && strings.HasPrefix(relative, ".kura/") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(relative, ".kura/") &&
			relative != ".kura/series.json" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir archive bytes: %v", err)
	}
	return total
}

func newCopyHandler(
	t *testing.T,
	progress *executor.FileProgressChannel,
	opener executor.DestinationOpener,
) executor.BackupActionHandler {
	t.Helper()
	handler, err := executor.NewBackupActionHandler(progress, opener, nil)
	if err != nil {
		t.Fatalf("NewBackupActionHandler: %v", err)
	}
	return handler
}

type recordingCapacityRecorder struct {
	tapeID tape.ID
	bytes  int64
}

func (r *recordingCapacityRecorder) RecordWrite(tapeID tape.ID, bytes int64) error {
	if r.tapeID == "" {
		r.tapeID = tapeID
	}
	if tapeID != r.tapeID {
		return errors.New("recorded writes for multiple cartridges")
	}
	r.bytes += bytes
	return nil
}

type wrappedDestination struct {
	*os.File
	syncErr     error
	closeErr    error
	beforeClose func() error
	write       func([]byte) (int, error)
}

func openWrappedDestination(path string) (*wrappedDestination, error) {
	destination, err := executor.OpenDestination(path)
	if err != nil {
		return nil, err
	}
	file, ok := destination.(*os.File)
	if !ok {
		return nil, errors.New("test destination is not *os.File")
	}
	return &wrappedDestination{File: file}, nil
}

func (d *wrappedDestination) Write(data []byte) (int, error) {
	if d.write != nil {
		return d.write(data)
	}
	return d.File.Write(data)
}

func (d *wrappedDestination) Sync() error {
	if d.syncErr != nil {
		return d.syncErr
	}
	return d.File.Sync()
}

func (d *wrappedDestination) Close() error {
	closeErr := d.File.Close()
	var beforeCloseErr error
	if d.beforeClose != nil {
		beforeCloseErr = d.beforeClose()
	}
	return errors.Join(closeErr, beforeCloseErr, d.closeErr)
}

type blockingSyncDrive struct {
	fixedCapacityDrive
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (d *blockingSyncDrive) Sync() error {
	d.once.Do(func() { close(d.started) })
	<-d.release
	return d.fixedCapacityDrive.Sync()
}

type executionOutcome struct {
	execution executor.PreparedExecution
	err       error
}

func assertItemFailureReason(
	t *testing.T,
	events []backupplan.Event,
	action backupplan.Action,
	reason backupplan.ItemReason,
) {
	t.Helper()
	assertEvent(t, events, backupplan.EventItemFailed, func(event backupplan.Event) bool {
		return event.MetadataRef == action.MetadataRef &&
			event.Generation == action.Generation &&
			event.Reason == string(reason)
	})
}

func assertItemFailureDetail(
	t *testing.T,
	fixture copyFixture,
	detail string,
) {
	t.Helper()
	assertEvent(
		t,
		fixture.sink.Events(),
		backupplan.EventItemFailed,
		func(event backupplan.Event) bool {
			return event.MetadataRef == fixture.action.MetadataRef &&
				event.Generation == fixture.action.Generation &&
				event.Reason == string(backupplan.ReasonWriteFailed) &&
				strings.Contains(event.Detail, detail)
		},
	)
}

func assertSnapshotAbsent(t *testing.T, fixture copyFixture) {
	t.Helper()
	_, err := os.Lstat(fixture.snapshotDir(t))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot Lstat error = %v, want os.ErrNotExist", err)
	}
}

var _ io.WriteCloser = (*wrappedDestination)(nil)

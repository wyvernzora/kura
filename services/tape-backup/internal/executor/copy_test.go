package executor_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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

func TestCopyRejectsEachPostCopyDriftAndHalts(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*testing.T, *copyFixture)
		wantReason backupplan.ItemReason
	}{
		{
			name: "live file added",
			mutate: func(t *testing.T, fixture *copyFixture) {
				writeFile(t, filepath.Join(fixture.seriesRoot, "late.ass"), []byte("late"))
			},
			wantReason: backupplan.ReasonPayloadDrift,
		},
		{
			name: "series root removed",
			mutate: func(t *testing.T, fixture *copyFixture) {
				if err := os.RemoveAll(fixture.seriesRoot); err != nil {
					t.Fatalf("RemoveAll series root error = %v", err)
				}
			},
			wantReason: backupplan.ReasonSeriesRootMissing,
		},
		{
			name: "generation changed",
			mutate: func(t *testing.T, fixture *copyFixture) {
				writeSeriesMetadata(t, fixture.seriesRoot, seriesMetadata{
					Generation:  fixture.action.Generation + 1,
					MetadataRef: fixture.action.MetadataRef,
				})
			},
			wantReason: backupplan.ReasonSeriesMoved,
		},
		{
			name: "metadata ref changed",
			mutate: func(t *testing.T, fixture *copyFixture) {
				writeSeriesMetadata(t, fixture.seriesRoot, seriesMetadata{
					Generation:  fixture.action.Generation,
					MetadataRef: "tvdb:different",
				})
			},
			wantReason: backupplan.ReasonSeriesMoved,
		},
		{
			name: "staging created",
			mutate: func(t *testing.T, fixture *copyFixture) {
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
			mutate: func(t *testing.T, fixture *copyFixture) {
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
			handler := fixture.handler(t, func(path string) (executor.Destination, error) {
				if strings.HasSuffix(path, "episode.mkv") {
					once.Do(func() { test.mutate(t, fixture) })
				}
				return executor.OpenDestination(path)
			})

			err := fixture.execute(t, handler)
			if !errors.Is(err, executor.ErrPlanFailed) {
				t.Fatalf("ExecutePreparedPlan error = %v, want ErrPlanFailed", err)
			}
			assertItemFailureReason(
				t,
				fixture.events(t),
				fixture.action,
				test.wantReason,
			)
			fixture.assertSnapshotAbsent(t)
		})
	}
}

func TestCopyRejectsNonNFCEntryBeforeOpeningDestination(t *testing.T) {
	fixture := newCopyFixture(t, nil)
	nfdPath := "Cafe\u0301.ass"
	if norm.NFC.IsNormalString(nfdPath) {
		t.Fatalf("test path %q is NFC-normalized", nfdPath)
	}
	writeFile(t, filepath.Join(fixture.seriesRoot, nfdPath), []byte("subtitle"))
	fixture.refreshAction(t)
	destinationsOpened := 0
	handler := fixture.handler(t, func(path string) (executor.Destination, error) {
		destinationsOpened++
		return executor.OpenDestination(path)
	})

	err := fixture.execute(t, handler)
	if !errors.Is(err, executor.ErrPlanFailed) {
		t.Fatalf("ExecutePreparedPlan error = %v, want ErrPlanFailed", err)
	}
	if destinationsOpened != 0 {
		t.Fatalf("destinations opened = %d, want 0", destinationsOpened)
	}
	assertItemFailureReason(
		t,
		fixture.events(t),
		fixture.action,
		backupplan.ReasonUnsupportedFileType,
	)
	fixture.assertSnapshotAbsent(t)
}

func TestCopyDurabilityBarrierRejectsCloseFailure(t *testing.T) {
	fixture := newCopyFixture(t, nil)
	handler := fixture.handler(t, func(path string) (executor.Destination, error) {
		destination, err := openWrappedDestination(path)
		if destination != nil && strings.HasSuffix(path, "episode.mkv") {
			destination.closeErr = errors.New("injected destination close failure")
		}
		return destination, err
	})

	err := fixture.execute(t, handler)
	if !errors.Is(err, executor.ErrPlanFailed) {
		t.Fatalf("ExecutePreparedPlan error = %v, want ErrPlanFailed", err)
	}
	assertItemFailureDetail(
		t,
		fixture.events(t),
		"executor: durability barrier: injected destination close failure",
	)
	fixture.assertSnapshotAbsent(t)
}

func TestCopyDurabilityBarrierRejectsByteCountMismatch(t *testing.T) {
	fixture := newCopyFixture(t, bytes.Repeat([]byte("payload"), 8192))
	sourcePath := filepath.Join(fixture.seriesRoot, "episode.mkv")
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile source error = %v", err)
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatalf("Stat source error = %v", err)
	}
	handler := fixture.handler(t, func(path string) (executor.Destination, error) {
		destination, err := openWrappedDestination(path)
		if err != nil || !strings.HasSuffix(path, "episode.mkv") {
			return destination, err
		}
		if err := os.Truncate(sourcePath, int64(len(sourceBytes)/2)); err != nil {
			t.Fatalf("Truncate source error = %v", err)
		}
		destination.beforeClose = func() error {
			if err := os.WriteFile(sourcePath, sourceBytes, 0o664); err != nil {
				return err
			}
			return os.Chtimes(sourcePath, sourceInfo.ModTime(), sourceInfo.ModTime())
		}
		return destination, nil
	})

	err = fixture.execute(t, handler)
	if !errors.Is(err, executor.ErrPlanFailed) {
		t.Fatalf("ExecutePreparedPlan error = %v, want ErrPlanFailed", err)
	}
	assertItemFailureDetail(
		t,
		fixture.events(t),
		fmt.Sprintf(
			"executor: durability barrier: wrote %d bytes, want source size %d",
			len(sourceBytes)/2,
			len(sourceBytes),
		),
	)
	fixture.assertSnapshotAbsent(t)
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

	if err := fixture.execute(t, fixture.handler(t, executor.OpenDestination)); err != nil {
		t.Fatalf("ExecutePreparedPlan error = %v", err)
	}

	manifest, err := tapemanifest.Read(fixture.snapshotDir(t))
	if err != nil {
		t.Fatalf("Read manifest error = %v", err)
	}
	paths := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		paths = append(paths, file.Path)
		data, err := os.ReadFile(filepath.Join(
			fixture.snapshotDir(t),
			"tree",
			filepath.FromSlash(file.Path),
		))
		if err != nil {
			t.Fatalf("ReadFile destination %q error = %v", file.Path, err)
		}
		sum := sha256.Sum256(data)
		wantHash := "sha256:" + hex.EncodeToString(sum[:])
		if file.Hash != wantHash {
			t.Fatalf("hash for %q = %q, want %q", file.Path, file.Hash, wantHash)
		}
	}
	want := []string{".kura/series.json", "a.ass", "episode.mkv"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("manifest paths = %#v, want %#v", paths, want)
	}
}

func TestCopyCancellationAbortsMidFileAndRemovesSnapshot(t *testing.T) {
	fixture := newCopyFixture(t, bytes.Repeat([]byte("x"), 1<<20))
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	handler := fixture.handler(t, func(path string) (executor.Destination, error) {
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
		result <- executor.ExecutePreparedPlan(
			ctx,
			fixture.prepared,
			0,
			1,
			handler,
		)
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ExecutePreparedPlan error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("copy did not abort within one second")
	}
	fixture.assertSnapshotAbsent(t)
}

func TestCopyProgressDropsOldestWithoutStalling(t *testing.T) {
	progress, err := executor.NewFileProgressChannel(1)
	if err != nil {
		t.Fatalf("NewFileProgressChannel error = %v", err)
	}
	fixture := newCopyFixture(t, bytes.Repeat([]byte("p"), 1<<20))
	handler, err := executor.NewBackupActionHandler(
		progress,
		executor.OpenDestination,
		fixture.drive,
	)
	if err != nil {
		t.Fatalf("NewBackupActionHandler error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- fixture.execute(t, handler)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExecutePreparedPlan error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stalled progress consumer blocked the copy")
	}
	select {
	case frame := <-progress.Frames():
		if frame.Path != "episode.mkv" ||
			frame.FileBytesWritten != frame.FileBytesTotal {
			t.Fatalf("latest progress = %#v, want completed episode.mkv", frame)
		}
	default:
		t.Fatal("progress channel is empty")
	}
}

func TestFileProgressChannelHidesWritableChannel(t *testing.T) {
	progress, err := executor.NewFileProgressChannel(1)
	if err != nil {
		t.Fatalf("NewFileProgressChannel error = %v", err)
	}
	progressType := reflect.TypeOf(progress)
	for index := range progressType.Elem().NumField() {
		field := progressType.Elem().Field(index)
		if field.Type.Kind() == reflect.Chan && field.IsExported() {
			t.Fatalf("progress channel field %q is exported", field.Name)
		}
	}
	if got := reflect.TypeOf(progress.Frames()).ChanDir(); got != reflect.RecvDir {
		t.Fatalf("Frames direction = %v, want receive-only", got)
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
		t.Fatalf("NewBackupActionHandler error = %v", err)
	}

	if err := fixture.execute(t, handler); err != nil {
		t.Fatalf("ExecutePreparedPlan error = %v", err)
	}
	if recorder.tapeID != fixture.prepared.Cartridge.TapeID {
		t.Fatalf(
			"recorded tapeID = %q, want %q",
			recorder.tapeID,
			fixture.prepared.Cartridge.TapeID,
		)
	}
	if recorder.bytes <= 0 {
		t.Fatalf("recorded bytes = %d, want positive", recorder.bytes)
	}
}

func TestCopyRemovesMarkerlessTargetBeforeWriting(t *testing.T) {
	fixture := newCopyFixture(t, nil)
	stale := filepath.Join(fixture.snapshotDir(t), "tree", "stale.mkv")
	writeFile(t, stale, []byte("stale"))

	if err := fixture.execute(
		t,
		fixture.handler(t, executor.OpenDestination),
	); err != nil {
		t.Fatalf("ExecutePreparedPlan error = %v", err)
	}
	if _, err := os.Lstat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Lstat stale target error = %v, want file does not exist", err)
	}
}

type copyFixture struct {
	prepared   executor.PreparedPlan
	action     backupplan.Action
	seriesRoot string
	stateRoot  string
	journal    *backupplan.Writer
	drive      *executor.DirectoryDrive
}

func newCopyFixture(t *testing.T, payload []byte) *copyFixture {
	t.Helper()
	if payload == nil {
		payload = []byte("payload")
	}
	session := newSessionFixture(t)
	action := writeSeries(t, session.libraryRoot, "Series", "tvdb:copy", 1)
	writeFile(t, filepath.Join(session.libraryRoot, "Series", "episode.mkv"), payload)
	digest, err := fingerprint.ComputeExcludingKura(filepath.Join(
		session.libraryRoot,
		"Series",
	))
	if err != nil {
		t.Fatalf("ComputeExcludingKura error = %v", err)
	}
	action.PayloadFingerprint = string(digest)
	action.Bytes = int64(len(payload))
	observed, err := tapecatalog.LoadObserved(session.stateRoot, firstVolume)
	if err != nil {
		t.Fatalf("LoadObserved error = %v", err)
	}
	plan := fillPlan(
		firstPlanID,
		firstVolume,
		"ABC123L6",
		nil,
		[]backupplan.Action{action},
	)
	journal := newSessionJournal(t, session.stateRoot, []string{plan.PlanID})
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Errorf("Close journal error = %v", err)
		}
	})
	return &copyFixture{
		prepared: executor.PreparedPlan{
			Drive:              session.drive,
			Journal:            journal,
			Cartridge:          executor.Cartridge{TapeID: "ABC123L6", Root: session.cartridgeRoot},
			Volume:             tapevolume.Volume{VolumeID: firstVolume, TapeID: "ABC123L6"},
			Plan:               plan,
			StateRoot:          session.stateRoot,
			LibraryRoot:        session.libraryRoot,
			CatalogSnapshots:   map[string]struct{}{},
			DebrisSnapshots:    map[string]struct{}{},
			CatalogObservation: observed,
		},
		action:     action,
		seriesRoot: filepath.Join(session.libraryRoot, "Series"),
		stateRoot:  session.stateRoot,
		journal:    journal,
		drive:      session.drive,
	}
}

func (f *copyFixture) refreshAction(t *testing.T) {
	t.Helper()
	digest, err := fingerprint.ComputeExcludingKura(f.seriesRoot)
	if err != nil {
		t.Fatalf("ComputeExcludingKura error = %v", err)
	}
	f.action.PayloadFingerprint = string(digest)
	for index := range f.prepared.Plan.Actions {
		if f.prepared.Plan.Actions[index].Type == backupplan.ActionBackup {
			f.prepared.Plan.Actions[index] = f.action
		}
	}
}

func (f *copyFixture) handler(
	t *testing.T,
	opener executor.DestinationOpener,
) executor.BackupActionHandler {
	t.Helper()
	handler, err := executor.NewBackupActionHandler(nil, opener, f.drive)
	if err != nil {
		t.Fatalf("NewBackupActionHandler error = %v", err)
	}
	return handler
}

func (f *copyFixture) execute(
	t *testing.T,
	handler executor.BackupActionHandler,
) error {
	t.Helper()
	return executor.ExecutePreparedPlan(t.Context(), f.prepared, 0, 1, handler)
}

func (f *copyFixture) snapshotDir(t *testing.T) string {
	t.Helper()
	dir, err := tapevolume.SnapshotDir(
		f.prepared.Cartridge.Root,
		f.action.MetadataRef,
		f.action.Generation,
	)
	if err != nil {
		t.Fatalf("SnapshotDir error = %v", err)
	}
	return dir
}

func (f *copyFixture) assertSnapshotAbsent(t *testing.T) {
	t.Helper()
	if _, err := os.Lstat(f.snapshotDir(t)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Lstat snapshot error = %v, want file does not exist", err)
	}
}

func (f *copyFixture) events(t *testing.T) []backupplan.Event {
	t.Helper()
	session, err := backupplan.ReadSession(f.stateRoot, sessionID)
	if err != nil {
		t.Fatalf("ReadSession error = %v", err)
	}
	return session.Events
}

type wrappedDestination struct {
	file        *os.File
	write       func([]byte) (int, error)
	closeErr    error
	beforeClose func() error
}

func openWrappedDestination(path string) (*wrappedDestination, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o664)
	if err != nil {
		return nil, err
	}
	return &wrappedDestination{file: file}, nil
}

func (d *wrappedDestination) Write(data []byte) (int, error) {
	if d.write != nil {
		return d.write(data)
	}
	return d.file.Write(data)
}

func (d *wrappedDestination) Close() error {
	if d.beforeClose != nil {
		if err := d.beforeClose(); err != nil {
			_ = d.file.Close()
			return err
		}
	}
	closeErr := d.file.Close()
	return errors.Join(closeErr, d.closeErr)
}

type recordingCapacityRecorder struct {
	tapeID tape.ID
	bytes  int64
}

func (r *recordingCapacityRecorder) RecordWrite(id tape.ID, bytes int64) error {
	r.tapeID = id
	r.bytes += bytes
	return nil
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
	events []backupplan.Event,
	want string,
) {
	t.Helper()
	assertEvent(t, events, backupplan.EventItemFailed, func(event backupplan.Event) bool {
		return event.Detail == want
	})
}

var (
	_ executor.Destination = (*wrappedDestination)(nil)
	_ io.Writer            = (*wrappedDestination)(nil)
)

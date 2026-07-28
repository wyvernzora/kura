package executor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wyvernzora/kura/services/tape-backup/internal/executor"
	"github.com/wyvernzora/kura/services/tape-backup/internal/snapshotname"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

const (
	firstPlanID  = "01JAY7M2K8Q3V5N0X2W4Z6B8D1"
	secondPlanID = "01JAY8B4N1R6X8Q0Z2C4E6G8J3"
	firstVolume  = "01J8ZQ7W5TWHA6R6J8X4QZ9Y7V"
	secondVolume = "01J8ZQ7W5TWHA6R6J8X4QZ9Y7W"
)

var fastSessionOptions = executor.SessionOptions{
	PollInterval: time.Millisecond,
	IdleTimeout:  50 * time.Millisecond,
	LibraryRoot:  "/library",
}

func TestRunSessionEncryptionInactiveFailsPlanAndSession(t *testing.T) {
	root := t.TempDir()
	stateRoot := t.TempDir()
	writeVolume(t, root, "ABC123L6", firstVolume)
	incomplete := makeSnapshot(t, root, "tvdb:100", 1, false)
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             root,
		Mounted:          true,
		EncryptionActive: false,
		Capacity:         1 << 20,
	}}, "ABC123L6")
	sink := &collectingSink{}
	journal := newSessionJournal(t, stateRoot)
	defer closeSessionJournal(t, journal)
	outbox := newOutboxWithJournal(t, journal, sink, 16)

	handlerCalled := false
	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{testPlan(firstPlanID, "ABC123L6", firstVolume)},
		outbox,
		func(context.Context, executor.PreparedPlan) error {
			handlerCalled = true
			return nil
		},
		fastSessionOptions,
	)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	closeOutbox(t, outbox)
	if handlerCalled {
		t.Fatal("prepared-plan handler was called")
	}
	assertExists(t, incomplete)
	assertNoEventType(t, sink.Events(), backupplan.EventSnapshotSwept)
	assertEvent(t, sink.Events(), backupplan.EventTapeRejected, func(event backupplan.Event) bool {
		return event.TapeID == "ABC123L6" && event.Reason == "encryption_inactive"
	})
	assertEvent(t, sink.Events(), backupplan.EventPlanFailed, func(event backupplan.Event) bool {
		return event.PlanID == firstPlanID &&
			event.Reason == "cartridge_rejected" &&
			strings.Contains(event.Detail, executor.ErrEncryptionInactive.Error())
	})

	session, err := backupplan.ReadSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if session.Terminal == nil {
		t.Fatal("terminal event is missing")
	}
	if got, want := session.Terminal.Reason, "plans_failed"; got != want {
		t.Fatalf("terminal reason = %q, want %q", got, want)
	}
	if got, want := session.Terminal.PlansCompleted, 0; got != want {
		t.Fatalf("terminal plansCompleted = %d, want %d", got, want)
	}
}

func TestRunSessionSweepRemovesOnlyUncommittedEntries(t *testing.T) {
	root := t.TempDir()
	writeVolume(t, root, "ABC123L6", firstVolume)

	committed := makeSnapshot(t, root, "tvdb:committed", 1, true)
	missingMarker := makeSnapshot(t, root, "tvdb:missing", 1, false)
	unparseable := filepath.Join(tapevolume.SnapshotsDir(root), "junk")
	mkdir(t, unparseable)

	markerSymlink := makeSnapshot(t, root, "tvdb:marker-link", 1, false)
	outsideMarker := filepath.Join(t.TempDir(), "complete.json")
	writeFile(t, outsideMarker, []byte("{}\n"))
	if err := os.Symlink(outsideMarker, filepath.Join(markerSymlink, "complete.json")); err != nil {
		t.Fatalf("Symlink completion marker: %v", err)
	}

	markerDirectory := makeSnapshot(t, root, "tvdb:marker-directory", 1, false)
	mkdir(t, filepath.Join(markerDirectory, "complete.json"))

	entryName := snapshotName(t, "tvdb:entry-link", 1)
	entrySymlink := filepath.Join(tapevolume.SnapshotsDir(root), entryName)
	entryTarget := filepath.Join(t.TempDir(), "valid-target")
	mkdir(t, entryTarget)
	writeFile(t, filepath.Join(entryTarget, "complete.json"), []byte("{}\n"))
	if err := os.Symlink(entryTarget, entrySymlink); err != nil {
		t.Fatalf("Symlink snapshot entry: %v", err)
	}

	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             root,
		Mounted:          true,
		EncryptionActive: true,
		Capacity:         1 << 20,
	}}, "ABC123L6")
	sink := &collectingSink{}
	outbox := newOutbox(t, sink, 16)
	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{testPlan(firstPlanID, "ABC123L6", firstVolume)},
		outbox,
		func(context.Context, executor.PreparedPlan) error { return nil },
		fastSessionOptions,
	)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	closeOutbox(t, outbox)

	assertExists(t, committed)
	assertNotExist(t, missingMarker)
	assertNotExist(t, unparseable)
	assertNotExist(t, markerSymlink)
	assertNotExist(t, markerDirectory)
	assertNotExist(t, entrySymlink)
	assertExists(t, entryTarget)

	got := sweptReasons(sink.Events())
	want := map[string]string{
		filepath.Base(missingMarker):   "missing_completion_marker",
		filepath.Base(unparseable):     "unparseable_name",
		filepath.Base(markerSymlink):   "invalid_completion_marker",
		filepath.Base(markerDirectory): "invalid_completion_marker",
		filepath.Base(entrySymlink):    "entry_not_directory",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("swept entries = %#v, want %#v", got, want)
	}
}

func TestRunSessionRejectsSweepFailureAndContinues(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	writeVolume(t, firstRoot, "ABC123L6", firstVolume)
	writeVolume(t, secondRoot, "DEF456L6", secondVolume)
	outside := t.TempDir()
	outsideEntry := filepath.Join(outside, "junk")
	mkdir(t, outsideEntry)
	if err := os.Symlink(outside, tapevolume.SnapshotsDir(firstRoot)); err != nil {
		t.Fatalf("Symlink snapshots directory: %v", err)
	}
	baseDrive := newDrive(t, []executor.DirectoryCartridge{
		{
			TapeID:           "ABC123L6",
			Root:             firstRoot,
			Mounted:          true,
			EncryptionActive: true,
			Capacity:         1 << 20,
		},
		{
			TapeID:           "DEF456L6",
			Root:             secondRoot,
			Mounted:          true,
			EncryptionActive: true,
			Capacity:         1 << 20,
		},
	}, "ABC123L6")
	drive := &switchingDrive{
		DirectoryDrive: baseDrive,
		next:           "DEF456L6",
		switchOnLoad:   2,
	}
	sink := &collectingSink{}
	outbox := newOutbox(t, sink, 16)
	var handled []string

	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{
			testPlan(firstPlanID, "ABC123L6", firstVolume),
			testPlan(secondPlanID, "DEF456L6", secondVolume),
		},
		outbox,
		func(_ context.Context, prepared executor.PreparedPlan) error {
			handled = append(handled, prepared.Plan.PlanID)
			return nil
		},
		fastSessionOptions,
	)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	closeOutbox(t, outbox)
	if want := []string{secondPlanID}; !reflect.DeepEqual(handled, want) {
		t.Fatalf("handled plans = %v, want %v", handled, want)
	}
	assertExists(t, outsideEntry)
	assertEvent(t, sink.Events(), backupplan.EventTapeRejected, func(event backupplan.Event) bool {
		return event.TapeID == "ABC123L6" &&
			event.Reason == "sweep_failed" &&
			event.Detail == "executor: snapshots directory must be a real directory"
	})
	assertEvent(t, sink.Events(), backupplan.EventPlanFailed, func(event backupplan.Event) bool {
		return event.PlanID == firstPlanID && event.Reason == "cartridge_rejected"
	})
}

func TestRunSessionIdentityFailureDoesNotSweep(t *testing.T) {
	root := t.TempDir()
	writeVolume(t, root, "ABC123L6", secondVolume)
	incomplete := makeSnapshot(t, root, "tvdb:100", 1, false)
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             root,
		Mounted:          true,
		EncryptionActive: true,
		Capacity:         1 << 20,
	}}, "ABC123L6")
	sink := &collectingSink{}
	outbox := newOutbox(t, sink, 16)

	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{testPlan(firstPlanID, "ABC123L6", firstVolume)},
		outbox,
		func(context.Context, executor.PreparedPlan) error {
			t.Fatal("prepared-plan handler was called")
			return nil
		},
		fastSessionOptions,
	)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	closeOutbox(t, outbox)
	assertExists(t, incomplete)
	assertNoEventType(t, sink.Events(), backupplan.EventSnapshotSwept)
	assertEvent(t, sink.Events(), backupplan.EventTapeRejected, func(event backupplan.Event) bool {
		return event.TapeID == "ABC123L6" && event.Reason == "volume_mismatch"
	})
}

func TestRunSessionRefusesSweepWithoutPrecedingAssertVolume(t *testing.T) {
	root := t.TempDir()
	writeVolume(t, root, "ABC123L6", firstVolume)
	incomplete := makeSnapshot(t, root, "tvdb:100", 1, false)
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             root,
		Mounted:          true,
		EncryptionActive: true,
		Capacity:         1 << 20,
	}}, "ABC123L6")
	sink := &collectingSink{}
	outbox := newOutbox(t, sink, 16)
	plan := testPlan(firstPlanID, "ABC123L6", firstVolume)
	plan.Actions = plan.Actions[1:]

	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{plan},
		outbox,
		func(context.Context, executor.PreparedPlan) error {
			t.Fatal("prepared-plan handler was called")
			return nil
		},
		fastSessionOptions,
	)
	assertExactError(
		t,
		err,
		"executor: plan "+firstPlanID+" must assert volume before backup",
	)
	closeOutbox(t, outbox)
	assertExists(t, incomplete)
	if len(sink.Events()) != 0 {
		t.Fatalf("events = %#v, want none", sink.Events())
	}
}

func TestRunSessionRefusesSweepWithoutAnyAssertVolume(t *testing.T) {
	outbox := newOutbox(t, &collectingSink{}, 4)
	plan := testPlan(firstPlanID, "ABC123L6", firstVolume)
	plan.Actions = []backupplan.Action{{
		Type: backupplan.ActionAssertInventory,
	}}

	err := executor.RunSession(
		t.Context(),
		faultDrive{err: errors.New("drive must not be called")},
		[]backupplan.Plan{plan},
		outbox,
		func(context.Context, executor.PreparedPlan) error {
			t.Fatal("prepared-plan handler was called")
			return nil
		},
		fastSessionOptions,
	)
	assertExactError(
		t,
		err,
		"executor: plan "+firstPlanID+" must assert volume before load-time sweep",
	)
	closeOutbox(t, outbox)
}

func TestRunSessionTwoCartridgesShrinksCandidateList(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	writeVolume(t, firstRoot, "ABC123L6", firstVolume)
	writeVolume(t, secondRoot, "DEF456L6", secondVolume)
	drive := newDrive(t, []executor.DirectoryCartridge{
		{
			TapeID:           "ABC123L6",
			Root:             firstRoot,
			Mounted:          true,
			EncryptionActive: true,
			Capacity:         1 << 20,
		},
		{
			TapeID:           "DEF456L6",
			Root:             secondRoot,
			Mounted:          true,
			EncryptionActive: true,
			Capacity:         1 << 20,
		},
	}, "ABC123L6")
	sink := &collectingSink{}
	outbox := newOutbox(t, sink, 16)
	var handled []string

	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{
			testPlan(firstPlanID, "ABC123L6", firstVolume),
			testPlan(secondPlanID, "DEF456L6", secondVolume),
		},
		outbox,
		func(_ context.Context, prepared executor.PreparedPlan) error {
			handled = append(handled, prepared.Plan.PlanID)
			if prepared.Plan.PlanID == firstPlanID {
				if err := drive.SelectLoaded("DEF456L6"); err != nil {
					return err
				}
			}
			return nil
		},
		fastSessionOptions,
	)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	closeOutbox(t, outbox)
	if want := []string{firstPlanID, secondPlanID}; !reflect.DeepEqual(handled, want) {
		t.Fatalf("handled plans = %v, want %v", handled, want)
	}

	var candidates [][]tape.ID
	for _, event := range sink.Events() {
		if event.Type == backupplan.EventWaitingForTape {
			candidates = append(candidates, event.Candidates)
		}
	}
	wantCandidates := [][]tape.ID{
		{"ABC123L6", "DEF456L6"},
		{"DEF456L6"},
	}
	if !reflect.DeepEqual(candidates, wantCandidates) {
		t.Fatalf("waiting candidates = %v, want %v", candidates, wantCandidates)
	}
}

func TestRunSessionRejectsMismatchedCartridgeAndContinues(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	writeVolume(t, firstRoot, "ABC123L6", secondVolume)
	writeVolume(t, secondRoot, "DEF456L6", secondVolume)
	baseDrive := newDrive(t, []executor.DirectoryCartridge{
		{
			TapeID:           "ABC123L6",
			Root:             firstRoot,
			Mounted:          true,
			EncryptionActive: true,
			Capacity:         1 << 20,
		},
		{
			TapeID:           "DEF456L6",
			Root:             secondRoot,
			Mounted:          true,
			EncryptionActive: true,
			Capacity:         1 << 20,
		},
	}, "ABC123L6")
	drive := &switchingDrive{
		DirectoryDrive: baseDrive,
		next:           "DEF456L6",
		switchOnLoad:   2,
	}
	sink := &collectingSink{}
	outbox := newOutbox(t, sink, 16)
	var handled []string

	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{
			testPlan(firstPlanID, "ABC123L6", firstVolume),
			testPlan(secondPlanID, "DEF456L6", secondVolume),
		},
		outbox,
		func(_ context.Context, prepared executor.PreparedPlan) error {
			handled = append(handled, prepared.Plan.PlanID)
			return nil
		},
		fastSessionOptions,
	)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	closeOutbox(t, outbox)
	if want := []string{secondPlanID}; !reflect.DeepEqual(handled, want) {
		t.Fatalf("handled plans = %v, want %v", handled, want)
	}
	assertEvent(t, sink.Events(), backupplan.EventTapeRejected, func(event backupplan.Event) bool {
		return event.TapeID == "ABC123L6" && event.Reason == "volume_mismatch"
	})
}

func TestRunSessionRejectRefreshesIdleDeadline(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	writeVolume(t, firstRoot, "ABC123L6", firstVolume)
	writeVolume(t, secondRoot, "DEF456L6", secondVolume)
	baseDrive := newDrive(t, []executor.DirectoryCartridge{
		{
			TapeID:           "ABC123L6",
			Root:             firstRoot,
			Mounted:          true,
			EncryptionActive: false,
			Capacity:         1 << 20,
		},
		{
			TapeID:           "DEF456L6",
			Root:             secondRoot,
			Mounted:          true,
			EncryptionActive: true,
			Capacity:         1 << 20,
		},
	}, "ABC123L6")
	drive := &scheduledDrive{
		DirectoryDrive: baseDrive,
		started:        time.Now(),
		first:          "ABC123L6",
		second:         "DEF456L6",
		firstAt:        200 * time.Millisecond,
		secondAt:       600 * time.Millisecond,
	}
	sink := &collectingSink{}
	outbox := newOutbox(t, sink, 16)
	options := executor.SessionOptions{
		PollInterval: 5 * time.Millisecond,
		IdleTimeout:  500 * time.Millisecond,
		LibraryRoot:  "/library",
	}
	var handled []string

	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{
			testPlan(firstPlanID, "ABC123L6", firstVolume),
			testPlan(secondPlanID, "DEF456L6", secondVolume),
		},
		outbox,
		func(_ context.Context, prepared executor.PreparedPlan) error {
			handled = append(handled, prepared.Plan.PlanID)
			return nil
		},
		options,
	)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	closeOutbox(t, outbox)
	if want := []string{secondPlanID}; !reflect.DeepEqual(handled, want) {
		t.Fatalf("handled plans = %v, want %v", handled, want)
	}
	assertEvent(t, sink.Events(), backupplan.EventTapeRejected, func(event backupplan.Event) bool {
		return event.TapeID == "ABC123L6" && event.Reason == "encryption_inactive"
	})
}

func TestDirectoryDriveRefusesUnmountedCartridge(t *testing.T) {
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             t.TempDir(),
		Mounted:          false,
		EncryptionActive: true,
		Capacity:         100,
	}}, "ABC123L6")

	_, _, capacityErr := drive.Capacity()
	if !errors.Is(capacityErr, executor.ErrNotMounted) {
		t.Fatalf("Capacity error = %v, want ErrNotMounted", capacityErr)
	}
	syncErr := drive.Sync()
	if !errors.Is(syncErr, executor.ErrNotMounted) {
		t.Fatalf("Sync error = %v, want ErrNotMounted", syncErr)
	}
}

func TestRunSessionDoesNotPrepareUnmountedMatchingCartridge(t *testing.T) {
	root := t.TempDir()
	stateRoot := t.TempDir()
	writeVolume(t, root, "ABC123L6", firstVolume)
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             root,
		Mounted:          false,
		EncryptionActive: true,
		Capacity:         100,
	}}, "ABC123L6")
	journal := newSessionJournal(t, stateRoot)
	defer closeSessionJournal(t, journal)
	outbox := newOutboxWithJournal(t, journal, &collectingSink{}, 16)
	options := fastSessionOptions
	options.IdleTimeout = 10 * time.Millisecond

	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{testPlan(firstPlanID, "ABC123L6", firstVolume)},
		outbox,
		func(context.Context, executor.PreparedPlan) error {
			t.Fatal("prepared-plan handler was called")
			return nil
		},
		options,
	)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	closeOutbox(t, outbox)
	session, err := backupplan.ReadSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if session.Terminal == nil {
		t.Fatal("terminal event is missing")
	}
	if got, want := session.Terminal.Reason, "idle_timeout"; got != want {
		t.Fatalf("terminal reason = %q, want %q", got, want)
	}
}

func TestDirectoryDriveRefusesWhenNothingLoaded(t *testing.T) {
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             t.TempDir(),
		Mounted:          true,
		EncryptionActive: true,
		Capacity:         100,
	}}, "")

	_, loadedErr := drive.Loaded()
	if !errors.Is(loadedErr, executor.ErrNoCartridge) {
		t.Fatalf("Loaded error = %v, want ErrNoCartridge", loadedErr)
	}
	_, encryptionErr := drive.EncryptionActive()
	if !errors.Is(encryptionErr, executor.ErrNoCartridge) {
		t.Fatalf("EncryptionActive error = %v, want ErrNoCartridge", encryptionErr)
	}
	_, _, capacityErr := drive.Capacity()
	if !errors.Is(capacityErr, executor.ErrNoCartridge) {
		t.Fatalf("Capacity error = %v, want ErrNoCartridge", capacityErr)
	}
	syncErr := drive.Sync()
	if !errors.Is(syncErr, executor.ErrNoCartridge) {
		t.Fatalf("Sync error = %v, want ErrNoCartridge", syncErr)
	}
}

func TestNewDirectoryDriveRejectsSymlinkRootWithoutCreatingArchive(t *testing.T) {
	outside := t.TempDir()
	root := filepath.Join(t.TempDir(), "cartridge")
	if err := os.Symlink(outside, root); err != nil {
		t.Fatalf("Symlink cartridge root: %v", err)
	}

	_, err := executor.NewDirectoryDrive([]executor.DirectoryCartridge{{
		TapeID:   "ABC123L6",
		Root:     root,
		Capacity: 100,
	}})
	assertExactError(
		t,
		err,
		"executor: directory cartridge ABC123L6 root must be a real directory",
	)
	assertNotExist(t, tapevolume.ArchiveDir(outside))
}

func TestRunSessionIdleTimeoutExits(t *testing.T) {
	root := t.TempDir()
	writeVolume(t, root, "ABC123L6", firstVolume)
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             root,
		Mounted:          true,
		EncryptionActive: true,
		Capacity:         100,
	}}, "")
	sink := &collectingSink{}
	outbox := newOutbox(t, sink, 16)
	options := fastSessionOptions
	options.IdleTimeout = 15 * time.Millisecond

	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{testPlan(firstPlanID, "ABC123L6", firstVolume)},
		outbox,
		func(context.Context, executor.PreparedPlan) error {
			t.Fatal("prepared-plan handler was called")
			return nil
		},
		options,
	)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	closeOutbox(t, outbox)
	events := sink.Events()
	if len(events) == 0 {
		t.Fatal("no events delivered")
	}
	terminal := events[len(events)-1]
	if terminal.Type != backupplan.EventTerminal ||
		terminal.State != "ended" ||
		terminal.Reason != "idle_timeout" ||
		terminal.PlansCompleted != 0 {
		t.Fatalf("terminal event = %#v", terminal)
	}
}

func TestRunSessionNormalCompletionRecordsTerminalEvent(t *testing.T) {
	root := t.TempDir()
	stateRoot := t.TempDir()
	writeVolume(t, root, "ABC123L6", firstVolume)
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             root,
		Mounted:          true,
		EncryptionActive: true,
		Capacity:         100,
	}}, "ABC123L6")
	journal := newSessionJournal(t, stateRoot)
	defer closeSessionJournal(t, journal)
	outbox := newOutboxWithJournal(t, journal, &collectingSink{}, 16)

	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{testPlan(firstPlanID, "ABC123L6", firstVolume)},
		outbox,
		func(context.Context, executor.PreparedPlan) error { return nil },
		fastSessionOptions,
	)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	closeOutbox(t, outbox)

	session, err := backupplan.ReadSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if session.Terminal == nil {
		t.Fatal("terminal event is missing")
	}
	if got, want := session.Terminal.State, "ended"; got != want {
		t.Fatalf("terminal state = %q, want %q", got, want)
	}
	if got, want := session.Terminal.Reason, "completed"; got != want {
		t.Fatalf("terminal reason = %q, want %q", got, want)
	}
	if got, want := session.Terminal.PlansCompleted, 1; got != want {
		t.Fatalf("terminal plansCompleted = %d, want %d", got, want)
	}
}

func TestEventOutboxStalledSinkBoundsRelayAndPersistsEvents(t *testing.T) {
	stateRoot := t.TempDir()
	journal := newSessionJournal(t, stateRoot)
	defer closeSessionJournal(t, journal)
	sink := newBlockingSink()
	const capacity = 2
	outbox := newOutboxWithJournal(t, journal, sink, capacity)
	if err := outbox.Emit(backupplan.Event{
		Type:   backupplan.EventPlanStarted,
		PlanID: firstPlanID,
	}); err != nil {
		t.Fatalf("Emit initial event: %v", err)
	}
	waitFor(t, time.Second, func() bool { return sink.Active() == 1 })

	const additionalEvents = 5
	for range additionalEvents {
		if err := outbox.Emit(backupplan.Event{
			Type:   backupplan.EventPlanStarted,
			PlanID: firstPlanID,
		}); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	time.Sleep(20 * time.Millisecond)
	if got := sink.MaxActive(); got != 1 {
		t.Fatalf("maximum concurrent sink calls = %d, want 1", got)
	}

	closeCtx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	err := outbox.Close(closeCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close error = %v, want DeadlineExceeded", err)
	}

	session, err := backupplan.ReadSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	const wantDurableEvents = 1 + additionalEvents + 1
	if len(session.Events) != wantDurableEvents {
		t.Fatalf("durable events = %d, want %d", len(session.Events), wantDurableEvents)
	}
	if got := countEventType(session.Events, backupplan.EventOutboxOverflow); got != 1 {
		t.Fatalf("durable outbox_overflow events = %d, want 1", got)
	}

	sink.Recover()
	closeOutbox(t, outbox)

	events := sink.Events()
	if len(events) != capacity+1 {
		t.Fatalf("delivered events = %d, want bounded %d", len(events), capacity+1)
	}
	if got := countEventType(events, backupplan.EventOutboxOverflow); got != 1 {
		t.Fatalf("delivered outbox_overflow events = %d, want 1", got)
	}
}

func TestEventOutboxFullRelayRecordsOverflowBeforeTerminal(t *testing.T) {
	stateRoot := t.TempDir()
	journal := newSessionJournal(t, stateRoot)
	defer closeSessionJournal(t, journal)
	sink := newBlockingSink()
	outbox := newOutboxWithJournal(t, journal, sink, 1)
	if err := outbox.Emit(backupplan.Event{
		Type:   backupplan.EventPlanStarted,
		PlanID: firstPlanID,
	}); err != nil {
		t.Fatalf("Emit initial event: %v", err)
	}
	waitFor(t, time.Second, func() bool { return sink.Active() == 1 })
	if err := outbox.Emit(backupplan.Event{
		Type:   backupplan.EventPlanStarted,
		PlanID: firstPlanID,
	}); err != nil {
		t.Fatalf("Emit queued event: %v", err)
	}
	if err := outbox.Emit(backupplan.Event{
		Type:           backupplan.EventTerminal,
		State:          "ended",
		Reason:         "idle_timeout",
		PlansCompleted: 0,
	}); err != nil {
		t.Fatalf("Emit terminal event: %v", err)
	}

	session, err := backupplan.ReadSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if got := countEventType(session.Events, backupplan.EventOutboxOverflow); got != 1 {
		t.Fatalf("durable outbox_overflow events = %d, want 1", got)
	}
	if session.Terminal == nil {
		t.Fatal("terminal event is missing")
	}
	if got, want := session.Terminal.Seq, uint64(4); got != want {
		t.Fatalf("terminal seq = %d, want %d", got, want)
	}

	sink.Recover()
	closeOutbox(t, outbox)
}

func TestNewEventOutboxRequiresSessionJournal(t *testing.T) {
	_, err := executor.NewEventOutbox(
		nil,
		&collectingSink{},
		executor.OutboxOptions{Capacity: 1, CallTimeout: time.Millisecond},
	)
	assertExactError(t, err, "executor: session journal is required")
}

func TestEventOutboxRefusesDeliveryWhenSessionAppendFails(t *testing.T) {
	journal := newSessionJournal(t, t.TempDir())
	closeSessionJournal(t, journal)
	sink := &collectingSink{}
	outbox := newOutboxWithJournal(t, journal, sink, 2)

	err := outbox.Emit(backupplan.Event{
		Type:   backupplan.EventPlanStarted,
		PlanID: firstPlanID,
	})
	assertExactError(
		t,
		err,
		"executor: append session event: backupplan: append to closed session",
	)
	closeOutbox(t, outbox)
	if got := len(sink.Events()); got != 0 {
		t.Fatalf("delivered events = %d, want 0", got)
	}
}

func TestEventOutboxResyncsSequenceAfterJournalSyncFailure(t *testing.T) {
	stateRoot := t.TempDir()
	journal := newSessionJournal(t, stateRoot)
	defer closeSessionJournal(t, journal)
	syncCalls := 0
	setSessionJournalSync(t, journal, func() error {
		syncCalls++
		if syncCalls == 1 {
			return errors.New("injected sync failure")
		}
		return nil
	})
	sink := &collectingSink{}
	outbox := newOutboxWithJournal(t, journal, sink, 2)

	err := outbox.Emit(backupplan.Event{
		Type:   backupplan.EventPlanStarted,
		PlanID: firstPlanID,
	})
	assertExactError(
		t,
		err,
		"executor: append session event: backupplan: sync session event: injected sync failure",
	)
	if err := outbox.Emit(backupplan.Event{
		Type:   backupplan.EventEncryptionVerified,
		PlanID: firstPlanID,
	}); err != nil {
		t.Fatalf("Emit after sync failure: %v", err)
	}
	closeOutbox(t, outbox)

	session, err := backupplan.ReadSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if got, want := len(session.Events), 2; got != want {
		t.Fatalf("durable events = %d, want %d", got, want)
	}
	if got, want := session.Events[1].Type, backupplan.EventEncryptionVerified; got != want {
		t.Fatalf("second durable event type = %q, want %q", got, want)
	}
	if got, want := session.Events[1].Seq, uint64(2); got != want {
		t.Fatalf("second durable event seq = %d, want %d", got, want)
	}
	delivered := sink.Events()
	if got, want := len(delivered), 1; got != want {
		t.Fatalf("delivered events = %d, want %d", got, want)
	}
	if got, want := delivered[0].Seq, uint64(2); got != want {
		t.Fatalf("delivered event seq = %d, want %d", got, want)
	}
}

func TestEventOutboxRefusesReofferAfterClose(t *testing.T) {
	outbox := newOutbox(t, &collectingSink{}, 2)
	closeOutbox(t, outbox)
	err := outbox.Reoffer(backupplan.Event{
		Type:   backupplan.EventPlanStarted,
		At:     time.Date(2026, 7, 27, 9, 21, 0, 0, time.UTC),
		Seq:    1,
		PlanID: firstPlanID,
	})
	assertExactError(t, err, "executor: re-offer to closed event outbox")
}

func TestEventOutboxStalledSinkBoundsHeap(t *testing.T) {
	goroutinesBefore := runtime.NumGoroutine()
	sink := newBlockingSink()
	outbox := newOutbox(t, sink, 8)
	if err := outbox.Emit(backupplan.Event{
		Type:   backupplan.EventPlanStarted,
		PlanID: firstPlanID,
	}); err != nil {
		t.Fatalf("Emit initial event: %v", err)
	}
	waitFor(t, time.Second, func() bool { return sink.Active() == 1 })

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	const eventCount = 10_000
	for range eventCount {
		if err := outbox.Emit(backupplan.Event{
			Type:   backupplan.EventPlanStarted,
			PlanID: firstPlanID,
		}); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	heapGrowth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf(
		"heap growth after %d blocked deliveries: %d bytes (%.1f MiB)",
		eventCount,
		heapGrowth,
		float64(heapGrowth)/(1<<20),
	)
	if heapGrowth > 2<<20 {
		t.Fatalf("heap growth = %d bytes, want at most %d", heapGrowth, 2<<20)
	}
	if got := runtime.NumGoroutine(); got > goroutinesBefore+1 {
		t.Fatalf("goroutines after sustained offering = %d, want at most %d", got, goroutinesBefore+1)
	}

	sink.Recover()
	closeOutbox(t, outbox)
}

func TestEventOutboxResumedJournalPersistsNewEvents(t *testing.T) {
	stateRoot := t.TempDir()
	journal := newSessionJournal(t, stateRoot)
	for seq := uint64(1); seq <= 4; seq++ {
		if err := journal.Append(backupplan.Event{
			Type:   backupplan.EventPlanStarted,
			At:     time.Date(2026, 7, 27, 9, 21, int(seq), 0, time.UTC),
			Seq:    seq,
			PlanID: firstPlanID,
		}); err != nil {
			t.Fatalf("Append seq %d: %v", seq, err)
		}
	}
	closeSessionJournal(t, journal)

	resumed, err := backupplan.OpenSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	defer closeSessionJournal(t, resumed)
	sink := &collectingSink{}
	outbox := newOutboxWithJournal(t, resumed, sink, 4)
	for range 3 {
		if err := outbox.Emit(backupplan.Event{
			Type:   backupplan.EventPlanStarted,
			PlanID: firstPlanID,
		}); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	closeOutbox(t, outbox)

	session, err := backupplan.ReadSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if got, want := len(session.Events), 7; got != want {
		t.Fatalf("durable events = %d, want %d", got, want)
	}
	for index, event := range session.Events[4:] {
		if got, want := event.Seq, uint64(index+5); got != want {
			t.Fatalf("new event %d seq = %d, want %d", index, got, want)
		}
	}
	if got, want := len(sink.Events()), 3; got != want {
		t.Fatalf("sink deliveries = %d, want %d", got, want)
	}
}

func TestEventOutboxReoffersPersistedEventsWithoutAppending(t *testing.T) {
	stateRoot := t.TempDir()
	journal := newSessionJournal(t, stateRoot)
	sink := newBlockingSink()
	outbox := newOutboxWithJournal(t, journal, sink, 2)
	if err := outbox.Emit(backupplan.Event{
		Type:   backupplan.EventPlanStarted,
		PlanID: firstPlanID,
	}); err != nil {
		t.Fatalf("Emit initial event: %v", err)
	}
	waitFor(t, time.Second, func() bool { return sink.Active() == 1 })
	for range 5 {
		if err := outbox.Emit(backupplan.Event{
			Type:   backupplan.EventPlanStarted,
			PlanID: firstPlanID,
		}); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	sink.Recover()
	closeOutbox(t, outbox)
	closeSessionJournal(t, journal)

	session, err := backupplan.ReadSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	sessionPath := filepath.Join(stateRoot, "sessions", firstPlanID+".jsonl")
	before, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("Stat session before replay: %v", err)
	}

	resumed, err := backupplan.OpenSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	defer closeSessionJournal(t, resumed)
	recoveredSink := &collectingSink{}
	replay := newOutboxWithJournal(t, resumed, recoveredSink, len(session.Events)+1)
	for _, event := range session.Events {
		if err := replay.Reoffer(event); err != nil {
			t.Fatalf("Re-offer seq %d: %v", event.Seq, err)
		}
	}
	after, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("Stat session after replay: %v", err)
	}
	if after.Size() != before.Size() {
		t.Fatalf("session size after replay = %d, want unchanged %d", after.Size(), before.Size())
	}
	if err := replay.Emit(backupplan.Event{
		Type:   backupplan.EventPlanStarted,
		PlanID: firstPlanID,
	}); err != nil {
		t.Fatalf("Emit after replay: %v", err)
	}
	closeOutbox(t, replay)

	resumedSession, err := backupplan.ReadSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("ReadSession after replay: %v", err)
	}
	if got := len(resumedSession.Events); got != len(session.Events)+1 {
		t.Fatalf("durable events after replay = %d, want %d", got, len(session.Events)+1)
	}
	if got, want := resumedSession.Events[len(resumedSession.Events)-1].Seq,
		uint64(len(session.Events)+1); got != want {
		t.Fatalf("new event seq after replay = %d, want %d", got, want)
	}
	if got := len(recoveredSink.Events()); got != len(session.Events)+1 {
		t.Fatalf("re-offered events delivered = %d, want %d", got, len(session.Events)+1)
	}
}

func TestEventOutboxReofferReportsFullRelayWithoutRecordingOverflow(t *testing.T) {
	stateRoot := t.TempDir()
	journal := newSessionJournal(t, stateRoot)
	for seq := uint64(1); seq <= 4; seq++ {
		if err := journal.Append(backupplan.Event{
			Type:   backupplan.EventPlanStarted,
			At:     time.Date(2026, 7, 27, 9, 21, int(seq), 0, time.UTC),
			Seq:    seq,
			PlanID: firstPlanID,
		}); err != nil {
			t.Fatalf("Append seq %d: %v", seq, err)
		}
	}
	closeSessionJournal(t, journal)
	sessionPath := filepath.Join(stateRoot, "sessions", firstPlanID+".jsonl")
	before, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("Stat session before re-offer: %v", err)
	}

	resumed, err := backupplan.OpenSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	defer closeSessionJournal(t, resumed)
	sink := newBlockingSink()
	outbox := newOutboxWithJournal(t, resumed, sink, 1)
	session, err := backupplan.ReadSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if err := outbox.Reoffer(session.Events[0]); err != nil {
		t.Fatalf("Re-offer seq 1: %v", err)
	}
	waitFor(t, time.Second, func() bool { return sink.Active() == 1 })
	if err := outbox.Reoffer(session.Events[1]); err != nil {
		t.Fatalf("Re-offer seq 2: %v", err)
	}
	for _, event := range session.Events[2:] {
		err := outbox.Reoffer(event)
		assertExactError(t, err, "executor: event relay is full")
	}

	after, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("Stat session after re-offer: %v", err)
	}
	if after.Size() != before.Size() {
		t.Fatalf("session size after relay overflow = %d, want unchanged %d", after.Size(), before.Size())
	}
	sink.Recover()
	closeOutbox(t, outbox)
	if got := countEventType(sink.Events(), backupplan.EventOutboxOverflow); got != 0 {
		t.Fatalf("re-offer delivered outbox_overflow events = %d, want 0", got)
	}
}

func TestEventOutboxCallTimeoutDropsRelayAttemptButPersistsEvent(t *testing.T) {
	stateRoot := t.TempDir()
	journal := newSessionJournal(t, stateRoot)
	defer closeSessionJournal(t, journal)
	sink := &timeoutOnceSink{}
	outbox := newOutboxWithJournal(t, journal, sink, 2)
	if err := outbox.Emit(backupplan.Event{
		Type:   backupplan.EventPlanStarted,
		PlanID: firstPlanID,
	}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	closeOutbox(t, outbox)
	if got := sink.calls.Load(); got != 1 {
		t.Fatalf("sink calls = %d, want 1", got)
	}
	if got := len(sink.Events()); got != 0 {
		t.Fatalf("delivered events = %d, want 0", got)
	}
	session, err := backupplan.ReadSession(stateRoot, firstPlanID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if got := len(session.Events); got != 1 {
		t.Fatalf("durable events = %d, want 1", got)
	}
}

func TestDirectoryDriveConsumedCapacityIsMonotonicAfterSweep(t *testing.T) {
	root := t.TempDir()
	writeVolume(t, root, "ABC123L6", firstVolume)
	incomplete := makeSnapshot(t, root, "tvdb:large", 1, false)
	writeFile(t, filepath.Join(incomplete, "payload.bin"), make([]byte, 4096))
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             root,
		Mounted:          true,
		EncryptionActive: true,
		Capacity:         1 << 20,
	}}, "ABC123L6")
	_, freeBefore, err := drive.Capacity()
	if err != nil {
		t.Fatalf("Capacity before sweep: %v", err)
	}
	sink := &collectingSink{}
	outbox := newOutbox(t, sink, 16)

	err = executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{testPlan(firstPlanID, "ABC123L6", firstVolume)},
		outbox,
		func(context.Context, executor.PreparedPlan) error { return nil },
		fastSessionOptions,
	)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	closeOutbox(t, outbox)
	_, freeAfter, err := drive.Capacity()
	if err != nil {
		t.Fatalf("Capacity after sweep: %v", err)
	}
	if freeAfter != freeBefore {
		t.Fatalf("free capacity after sweep = %d, want monotonic %d", freeAfter, freeBefore)
	}
}

func TestDirectoryDriveFaultInjection(t *testing.T) {
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             t.TempDir(),
		Mounted:          true,
		EncryptionActive: true,
		Capacity:         100,
	}}, "ABC123L6")
	sentinel := errors.New("injected")

	drive.SetFaults(executor.DriveFaults{Loaded: sentinel})
	_, err := drive.Loaded()
	if !errors.Is(err, sentinel) {
		t.Fatalf("Loaded error = %v, want injected", err)
	}
	drive.SetFaults(executor.DriveFaults{EncryptionActive: sentinel})
	_, err = drive.EncryptionActive()
	if !errors.Is(err, sentinel) {
		t.Fatalf("EncryptionActive error = %v, want injected", err)
	}
	drive.SetFaults(executor.DriveFaults{Capacity: sentinel})
	_, _, err = drive.Capacity()
	if !errors.Is(err, sentinel) {
		t.Fatalf("Capacity error = %v, want injected", err)
	}
	drive.SetFaults(executor.DriveFaults{Sync: sentinel})
	err = drive.Sync()
	if !errors.Is(err, sentinel) {
		t.Fatalf("Sync error = %v, want injected", err)
	}
}

func TestRunSessionRefusesOutOfScopeActionWholeAndNamed(t *testing.T) {
	sink := &collectingSink{}
	outbox := newOutbox(t, sink, 4)
	plan := testPlan(firstPlanID, "ABC123L6", firstVolume)
	plan.Actions = append(plan.Actions, backupplan.Action{Type: backupplan.ActionVerify})

	err := executor.RunSession(
		t.Context(),
		faultDrive{err: errors.New("drive must not be called")},
		[]backupplan.Plan{plan},
		outbox,
		func(context.Context, executor.PreparedPlan) error {
			t.Fatal("prepared-plan handler was called")
			return nil
		},
		fastSessionOptions,
	)
	assertExactError(
		t,
		err,
		`executor: plan `+firstPlanID+` contains out-of-scope action "verify"`,
	)
	closeOutbox(t, outbox)
	if len(sink.Events()) != 0 {
		t.Fatalf("events = %#v, want none", sink.Events())
	}
}

func TestRunSessionRefusesUnknownActionWholeAndNamed(t *testing.T) {
	sink := &collectingSink{}
	outbox := newOutbox(t, sink, 4)
	plan := testPlan(firstPlanID, "ABC123L6", firstVolume)
	plan.Actions = append(plan.Actions, backupplan.Action{
		Type: backupplan.ActionType("detonate"),
	})

	err := executor.RunSession(
		t.Context(),
		faultDrive{err: errors.New("drive must not be called")},
		[]backupplan.Plan{plan},
		outbox,
		func(context.Context, executor.PreparedPlan) error {
			t.Fatal("prepared-plan handler was called")
			return nil
		},
		fastSessionOptions,
	)
	assertExactError(
		t,
		err,
		`executor: plan `+firstPlanID+` contains unknown action "detonate"`,
	)
	closeOutbox(t, outbox)
	if len(sink.Events()) != 0 {
		t.Fatalf("events = %#v, want none", sink.Events())
	}
}

type faultDrive struct {
	err error
}

func (d faultDrive) Loaded() (executor.Cartridge, error) { return executor.Cartridge{}, d.err }
func (faultDrive) EncryptionActive() (bool, error)       { return false, nil }
func (faultDrive) Capacity() (total, free int64, err error) {
	return 0, 0, nil
}
func (faultDrive) Sync() error { return nil }

type switchingDrive struct {
	*executor.DirectoryDrive
	next         tape.ID
	loads        int
	switchOnLoad int
}

func (d *switchingDrive) Loaded() (executor.Cartridge, error) {
	d.loads++
	if d.loads == d.switchOnLoad {
		if err := d.SelectLoaded(d.next); err != nil {
			return executor.Cartridge{}, err
		}
	}
	return d.DirectoryDrive.Loaded()
}

type scheduledDrive struct {
	*executor.DirectoryDrive
	started  time.Time
	first    tape.ID
	second   tape.ID
	firstAt  time.Duration
	secondAt time.Duration
}

func (d *scheduledDrive) Loaded() (executor.Cartridge, error) {
	elapsed := time.Since(d.started)
	switch {
	case elapsed < d.firstAt:
		d.ClearLoaded()
	case elapsed < d.secondAt:
		if err := d.SelectLoaded(d.first); err != nil {
			return executor.Cartridge{}, err
		}
	default:
		if err := d.SelectLoaded(d.second); err != nil {
			return executor.Cartridge{}, err
		}
	}
	return d.DirectoryDrive.Loaded()
}

type collectingSink struct {
	mu     sync.Mutex
	events []backupplan.Event
}

func (s *collectingSink) Deliver(_ context.Context, event backupplan.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *collectingSink) Events() []backupplan.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]backupplan.Event(nil), s.events...)
}

type blockingSink struct {
	release   chan struct{}
	recovered atomic.Bool
	active    atomic.Int32
	maxActive atomic.Int32
	mu        sync.Mutex
	events    []backupplan.Event
}

type timeoutOnceSink struct {
	calls  atomic.Int32
	mu     sync.Mutex
	events []backupplan.Event
}

func (s *timeoutOnceSink) Deliver(ctx context.Context, event backupplan.Event) error {
	if s.calls.Add(1) == 1 {
		<-ctx.Done()
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *timeoutOnceSink) Events() []backupplan.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]backupplan.Event(nil), s.events...)
}

func newBlockingSink() *blockingSink {
	return &blockingSink{release: make(chan struct{})}
}

func (s *blockingSink) Deliver(_ context.Context, event backupplan.Event) error {
	active := s.active.Add(1)
	for {
		maxActive := s.maxActive.Load()
		if active <= maxActive || s.maxActive.CompareAndSwap(maxActive, active) {
			break
		}
	}
	if !s.recovered.Load() {
		<-s.release
	}
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
	s.active.Add(-1)
	return nil
}

func (s *blockingSink) Recover() {
	if s.recovered.CompareAndSwap(false, true) {
		close(s.release)
	}
}

func (s *blockingSink) Active() int32 {
	return s.active.Load()
}

func (s *blockingSink) MaxActive() int32 {
	return s.maxActive.Load()
}

func (s *blockingSink) Events() []backupplan.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]backupplan.Event(nil), s.events...)
}

func countEventType(events []backupplan.Event, eventType backupplan.EventType) int {
	count := 0
	for _, event := range events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

func testPlan(planID string, tapeID tape.ID, volumeID volume.ID) backupplan.Plan {
	return backupplan.Plan{
		PlanID: planID,
		Target: backupplan.Target{TapeID: tapeID},
		Actions: []backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: volumeID},
			{
				Type:               backupplan.ActionBackup,
				MetadataRef:        "tvdb:370070",
				RootPath:           "Anime Series",
				Generation:         1,
				PayloadFingerprint: "sha256:" + strings.Repeat("0", 64),
				Bytes:              0,
			},
		},
	}
}

func newDrive(
	t *testing.T,
	cartridges []executor.DirectoryCartridge,
	loaded tape.ID,
) *executor.DirectoryDrive {
	t.Helper()
	drive, err := executor.NewDirectoryDrive(cartridges)
	if err != nil {
		t.Fatalf("NewDirectoryDrive: %v", err)
	}
	if loaded != "" {
		if err := drive.SelectLoaded(loaded); err != nil {
			t.Fatalf("SelectLoaded: %v", err)
		}
	}
	return drive
}

func newOutbox(t *testing.T, sink executor.EventSink, capacity int) *executor.EventOutbox {
	t.Helper()
	journal := newSessionJournal(t, t.TempDir())
	t.Cleanup(func() {
		closeSessionJournal(t, journal)
	})
	return newOutboxWithJournal(t, journal, sink, capacity)
}

func newOutboxWithJournal(
	t *testing.T,
	journal *backupplan.Writer,
	sink executor.EventSink,
	capacity int,
) *executor.EventOutbox {
	t.Helper()
	outbox, err := executor.NewEventOutbox(journal, sink, executor.OutboxOptions{
		Capacity:    capacity,
		CallTimeout: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewEventOutbox: %v", err)
	}
	return outbox
}

func newSessionJournal(t *testing.T, stateRoot string) *backupplan.Writer {
	t.Helper()
	journal, err := backupplan.CreateSession(stateRoot, backupplan.Header{
		SessionID: firstPlanID,
		Executor: backupplan.Creator{
			Version: "v0.6.0",
			Host:    "tape-vm",
		},
		StartedAt:  time.Date(2026, 7, 27, 9, 20, 1, 0, time.UTC),
		ReadyPlans: []string{firstPlanID},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return journal
}

func closeSessionJournal(t *testing.T, journal *backupplan.Writer) {
	t.Helper()
	if err := journal.Close(); err != nil {
		t.Fatalf("Close session journal: %v", err)
	}
}

func setSessionJournalSync(
	t *testing.T,
	journal *backupplan.Writer,
	sync func() error,
) {
	t.Helper()
	field := reflect.ValueOf(journal).Elem().FieldByName("sync")
	if !field.IsValid() || !field.CanAddr() {
		t.Fatal("backupplan.Writer sync seam is unavailable")
	}
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).
		Elem().
		Set(reflect.ValueOf(sync))
}

func closeOutbox(t *testing.T, outbox *executor.EventOutbox) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := outbox.Close(ctx); err != nil {
		t.Fatalf("Close outbox: %v", err)
	}
}

func writeVolume(t *testing.T, root string, tapeID tape.ID, volumeID volume.ID) {
	t.Helper()
	err := tapevolume.Write(root, tapevolume.Volume{
		VolumeID:  volumeID,
		TapeID:    tapeID,
		CreatedAt: time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Write volume: %v", err)
	}
}

func makeSnapshot(
	t *testing.T,
	root, metadataRef string,
	generation int,
	committed bool,
) string {
	t.Helper()
	path := filepath.Join(
		tapevolume.SnapshotsDir(root),
		snapshotName(t, metadataRef, generation),
	)
	mkdir(t, path)
	if committed {
		writeFile(t, filepath.Join(path, "complete.json"), []byte("{}\n"))
	}
	return path
}

func snapshotName(t *testing.T, metadataRef string, generation int) string {
	t.Helper()
	name, err := snapshotname.Format(metadataRef, generation)
	if err != nil {
		t.Fatalf("Format snapshot name: %v", err)
	}
	return name
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o775); err != nil {
		t.Fatalf("MkdirAll %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, data, 0o664); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("Lstat %s: %v", path, err)
	}
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	_, err := os.Lstat(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Lstat %s error = %v, want os.ErrNotExist", path, err)
	}
}

func assertExactError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %q", want)
	}
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func assertNoEventType(
	t *testing.T,
	events []backupplan.Event,
	eventType backupplan.EventType,
) {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType {
			t.Fatalf("unexpected %s event: %#v", eventType, event)
		}
	}
}

func assertEvent(
	t *testing.T,
	events []backupplan.Event,
	eventType backupplan.EventType,
	matches func(backupplan.Event) bool,
) {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType && matches(event) {
			return
		}
	}
	t.Fatalf("no matching %s event in %#v", eventType, events)
}

func sweptReasons(events []backupplan.Event) map[string]string {
	reasons := make(map[string]string)
	for _, event := range events {
		if event.Type == backupplan.EventSnapshotSwept {
			reasons[event.Entry] = event.Reason
		}
	}
	return reasons
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true")
		}
		time.Sleep(time.Millisecond)
	}
}

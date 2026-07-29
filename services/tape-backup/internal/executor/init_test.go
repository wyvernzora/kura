package executor_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/executor"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapecatalog"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

func TestRunSessionInitHappyPathFormatsStampsInstallsAndFills(t *testing.T) {
	fixture := newInitFixture(t, executor.LoadedIdentityUnidentified, false)
	action := writeSeries(t, fixture.libraryRoot, "Series A", "tvdb:a", 1)
	fixture.plan = initPlan([]backupplan.Action{action})
	fixture.promote(t)
	drive := newRitualDrive(fixture.drive)
	normal := fixture.backupHandler(t)

	err := fixture.run(t, drive, func(
		ctx context.Context,
		request executor.BackupActionRequest,
	) error {
		if _, err := tapecatalog.LoadObserved(fixture.stateRoot, firstVolume); err != nil {
			t.Fatalf("catalog entry was not installed before fill: %v", err)
		}
		return normal(ctx, request)
	})
	if err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	if got := drive.callSequence(); !isSubsequence(
		got,
		[]string{"encryption", "format", "encryption", "stamp", "sync"},
	) {
		t.Fatalf("drive call sequence = %v, want encryption/format/encryption/stamp/sync", got)
	}
	assertCatalogSnapshot(
		t,
		fixture.stateRoot,
		firstVolume,
		snapshotName(t, action.MetadataRef, action.Generation),
		true,
	)
	entry, err := tapecatalog.LoadObserved(fixture.stateRoot, firstVolume)
	if err != nil {
		t.Fatalf("LoadObserved error = %v", err)
	}
	if entry.TapeID != "ABC123L6" {
		t.Fatalf("catalog tapeID = %s, want ABC123L6", entry.TapeID)
	}
	if _, err := backupplan.ReadDone(fixture.stateRoot, firstPlanID); err != nil {
		t.Fatalf("ReadDone error = %v", err)
	}
	session := fixture.readSession(t)
	if got := countEvents(session.Events, backupplan.EventEncryptionVerified); got != 2 {
		t.Fatalf("encryption_verified events = %d, want 2", got)
	}
}

func TestRunSessionInitSerialSwapRefusesBeforeFormat(t *testing.T) {
	fixture := newInitFixture(t, executor.LoadedIdentityUnidentified, false)
	fixture.plan = initPlan(nil)
	fixture.promote(t)
	drive := newRitualDrive(fixture.drive)
	drive.mediumSerial = "MAM-SWAPPED-0002"

	if err := fixture.run(t, drive, fixture.backupHandler(t)); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	if drive.countCall("format") != 0 {
		t.Fatal("Format was called after medium serial mismatch")
	}
	assertPlanDiscarded(t, fixture.stateRoot, firstPlanID)
	session := fixture.readSession(t)
	const want = "executor: plan " + firstPlanID +
		` pre-state mismatch: loaded medium serial "MAM-SWAPPED-0002", want "MAM-ABC123-0001"`
	assertExactPlanFailure(t, session, want)
}

func TestRunSessionInitPreflightRefusesOversizedFillBeforeFormat(t *testing.T) {
	fixture := newInitFixture(t, executor.LoadedIdentityUnidentified, false)
	action := writeSeries(t, fixture.libraryRoot, "Series A", "tvdb:a", 1)
	action.Bytes = 2_500_000_000_001
	fixture.plan = initPlan([]backupplan.Action{action})
	fixture.promote(t)
	drive := newRitualDrive(fixture.drive)

	if err := fixture.run(t, drive, fixture.backupHandler(t)); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	if drive.countCall("format") != 0 {
		t.Fatal("Format was called after init fill set exceeded nominal capacity")
	}
	session := fixture.readSession(t)
	const want = "executor: init fill set needs 2500000000001 bytes; " +
		"0-byte margin, 2500000000000 bytes nominal capacity"
	assertExactPlanFailure(t, session, want)
}

func TestRunSessionInitEncryptionInactiveAfterRemountRetiresBeforeStamp(t *testing.T) {
	fixture := newInitFixture(t, executor.LoadedIdentityUnidentified, false)
	fixture.plan = initPlan(nil)
	fixture.promote(t)
	drive := newRitualDrive(fixture.drive)
	drive.inactiveOnEncryptionCheck = 2

	if err := fixture.run(t, drive, fixture.backupHandler(t)); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	if drive.countCall("format") != 1 {
		t.Fatalf("Format calls = %d, want 1", drive.countCall("format"))
	}
	if drive.countCall("stamp") != 0 {
		t.Fatal("StampIdentity was called after the second encryption gate failed")
	}
	assertPlanDiscarded(t, fixture.stateRoot, firstPlanID)
	identity, err := fixture.drive.LoadedIdentity()
	if err != nil {
		t.Fatalf("LoadedIdentity error = %v", err)
	}
	if identity.State != executor.LoadedIdentityUnidentified {
		t.Fatalf("identity state = %q, want unidentified", identity.State)
	}
}

func TestRunSessionInitSyncFailureLeavesStampedUncatalogedWitness(t *testing.T) {
	fixture := newInitFixture(t, executor.LoadedIdentityUnidentified, false)
	fixture.plan = initPlan(nil)
	fixture.promote(t)
	drive := newRitualDrive(fixture.drive)
	fixture.drive.SetFaults(executor.DriveFaults{
		Sync: errors.New("injected stamp sync failure"),
	})

	if err := fixture.run(t, drive, fixture.backupHandler(t)); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	assertHealthyEmptyWitness(t, fixture.cartridgeRoot)
	header, err := tapevolume.Read(fixture.cartridgeRoot)
	if err != nil {
		t.Fatalf("Read stamped header error = %v", err)
	}
	if header.VolumeID != firstVolume || header.TapeID != "ABC123L6" {
		t.Fatalf("stamped header = %#v", header)
	}
	if active, err := tapecatalog.ListActive(fixture.stateRoot); err != nil {
		t.Fatalf("ListActive error = %v", err)
	} else if len(active) != 0 {
		t.Fatalf("active volumes = %v, want none", active)
	}
	assertPlanDiscarded(t, fixture.stateRoot, firstPlanID)
}

func TestRunSessionInitHealthyEmptyWitnessProceeds(t *testing.T) {
	fixture := newInitFixture(t, executor.LoadedIdentityIdentified, true)
	fixture.plan = initPlanForVolume(nil, secondVolume)
	fixture.promote(t)
	drive := newRitualDrive(fixture.drive)
	drive.unmountOnCapacityCall = 2

	if err := fixture.run(t, drive, fixture.backupHandler(t)); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	if _, err := backupplan.ReadDone(fixture.stateRoot, firstPlanID); err != nil {
		t.Fatalf("ReadDone error = %v", err)
	}
	if drive.countCall("format") != 1 {
		t.Fatalf("Format calls = %d, want 1", drive.countCall("format"))
	}
	if _, err := tapecatalog.LoadObserved(fixture.stateRoot, secondVolume); err != nil {
		t.Fatalf("LoadObserved fresh admitted volume error = %v", err)
	}
}

func TestRunSessionInitRecognizedZeroWithoutWitnessRefuses(t *testing.T) {
	fixture := newInitFixture(t, executor.LoadedIdentityIdentified, true)
	installCatalogVolume(
		t,
		fixture.stateRoot,
		fixture.cartridgeRoot,
		firstVolume,
		"ABC123L6",
		1<<20,
		1<<20,
	)
	if err := os.RemoveAll(tapevolume.SnapshotsDir(fixture.cartridgeRoot)); err != nil {
		t.Fatalf("RemoveAll snapshots error = %v", err)
	}
	fixture.plan = initPlanForVolume(nil, secondVolume)
	fixture.promote(t)
	drive := newRitualDrive(fixture.drive)

	if err := fixture.run(t, drive, fixture.backupHandler(t)); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	if drive.countCall("format") != 0 {
		t.Fatal("Format was called without the positive healthy-empty witness")
	}
	session := fixture.readSession(t)
	const want = "executor: plan " + firstPlanID +
		" divergence_deferred: volume " + string(firstVolume) +
		" healthy-empty witness: snapshots namespace is absent"
	assertExactPlanFailure(t, session, want)
}

func TestRunSessionInitWitnessEnumerationErrorRefuses(t *testing.T) {
	fixture := newInitFixture(t, executor.LoadedIdentityIdentified, true)
	snapshotsDir := tapevolume.SnapshotsDir(fixture.cartridgeRoot)
	if err := os.Chmod(snapshotsDir, 0); err != nil {
		t.Fatalf("Chmod snapshots error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(snapshotsDir, 0o775)
	})
	fixture.plan = initPlanForVolume(nil, secondVolume)
	fixture.promote(t)
	drive := newRitualDrive(fixture.drive)
	drive.fixedCapacity = true

	if err := fixture.run(t, drive, fixture.backupHandler(t)); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	if drive.countCall("format") != 0 {
		t.Fatal("Format was called after snapshot enumeration failed")
	}
	session := fixture.readSession(t)
	want := "executor: plan " + firstPlanID +
		" adoption_deferred: volume " + string(firstVolume) +
		" healthy-empty witness: enumerate snapshots namespace: open " +
		snapshotsDir + ": permission denied"
	assertExactPlanFailure(t, session, want)
}

func TestRunSessionInitWitnessWithCommittedSnapshotRefuses(t *testing.T) {
	fixture := newInitFixture(t, executor.LoadedIdentityIdentified, true)
	action := backupAction("tvdb:existing", "Existing", 1, "sha256:existing", 1)
	writeCommittedSnapshot(t, fixture.cartridgeRoot, action, "existing")
	fixture.plan = initPlanForVolume(nil, secondVolume)
	fixture.promote(t)
	drive := newRitualDrive(fixture.drive)

	if err := fixture.run(t, drive, fixture.backupHandler(t)); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	if drive.countCall("format") != 0 {
		t.Fatal("Format was called for media with a committed snapshot")
	}
	session := fixture.readSession(t)
	const want = "executor: plan " + firstPlanID +
		" adoption_deferred: volume " + string(firstVolume) +
		" has 1 committed snapshots"
	assertExactPlanFailure(t, session, want)
}

type initFixture struct {
	stateRoot     string
	libraryRoot   string
	cartridgeRoot string
	drive         *executor.DirectoryDrive
	plan          backupplan.Plan
	journal       *backupplan.Writer
}

func newInitFixture(
	t *testing.T,
	identityState executor.LoadedIdentityState,
	mounted bool,
) *initFixture {
	t.Helper()
	stateRoot := t.TempDir()
	libraryRoot := t.TempDir()
	cartridgeRoot := t.TempDir()
	if identityState == executor.LoadedIdentityIdentified {
		writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	}
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             cartridgeRoot,
		Mounted:          mounted,
		IdentityState:    identityState,
		MediumSerial:     "MAM-ABC123-0001",
		EncryptionActive: true,
		Capacity:         1 << 20,
	}}, "ABC123L6")
	return &initFixture{
		stateRoot:     stateRoot,
		libraryRoot:   libraryRoot,
		cartridgeRoot: cartridgeRoot,
		drive:         drive,
	}
}

func (f *initFixture) promote(t *testing.T) {
	t.Helper()
	if err := backupplan.Draft(f.stateRoot, f.plan); err != nil {
		t.Fatalf("Draft error = %v", err)
	}
	if err := backupplan.Approve(f.stateRoot, f.plan.PlanID); err != nil {
		t.Fatalf("Approve error = %v", err)
	}
	f.journal = newSessionJournal(t, f.stateRoot, []string{f.plan.PlanID})
}

func (f *initFixture) backupHandler(t *testing.T) executor.BackupActionHandler {
	t.Helper()
	handler, err := executor.NewBackupActionHandler(
		nil,
		executor.OpenDestination,
		f.drive,
	)
	if err != nil {
		t.Fatalf("NewBackupActionHandler error = %v", err)
	}
	return handler
}

func (f *initFixture) run(
	t *testing.T,
	drive executor.Drive,
	handler executor.BackupActionHandler,
) error {
	t.Helper()
	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{f.plan},
		f.journal,
		handler,
		executor.SessionOptions{
			PollInterval:    time.Millisecond,
			IdleTimeout:     50 * time.Millisecond,
			StateRoot:       f.stateRoot,
			LibraryRoot:     f.libraryRoot,
			FreeSpaceMargin: 0,
			FlushCadence:    1,
		},
	)
	if closeErr := f.journal.Close(); closeErr != nil {
		t.Fatalf("Close journal error = %v", closeErr)
	}
	return err
}

func (f *initFixture) readSession(t *testing.T) backupplan.Session {
	t.Helper()
	session, err := backupplan.ReadSession(f.stateRoot, sessionID)
	if err != nil {
		t.Fatalf("ReadSession error = %v", err)
	}
	return session
}

func initPlan(backups []backupplan.Action) backupplan.Plan {
	return initPlanForVolume(backups, firstVolume)
}

func initPlanForVolume(
	backups []backupplan.Action,
	admittedVolume volume.ID,
) backupplan.Plan {
	trailing := make([]string, 0, len(backups))
	for _, action := range backups {
		trailing = append(
			trailing,
			mustSnapshotName(action.MetadataRef, action.Generation),
		)
	}
	slices.Sort(trailing)
	actions := []backupplan.Action{
		{Type: backupplan.ActionReformat},
		{Type: backupplan.ActionAdmit, VolumeID: admittedVolume},
	}
	actions = append(actions, backups...)
	actions = append(actions, backupplan.Action{
		Type:      backupplan.ActionAssertInventory,
		Snapshots: trailing,
	})
	return backupplan.Plan{
		PlanID:    firstPlanID,
		CreatedAt: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		CreatedBy: backupplan.Creator{Version: "v0.6.0", Host: "tape-vm"},
		Target: backupplan.Target{
			TapeID:       "ABC123L6",
			MediumSerial: "MAM-ABC123-0001",
		},
		Actions: actions,
	}
}

type ritualDrive struct {
	*executor.DirectoryDrive
	mu                        sync.Mutex
	calls                     []string
	capacityCalls             int
	unmountOnCapacityCall     int
	inactiveOnEncryptionCheck int
	encryptionChecks          int
	fixedCapacity             bool
	mediumSerial              string
}

func newRitualDrive(drive *executor.DirectoryDrive) *ritualDrive {
	return &ritualDrive{DirectoryDrive: drive}
}

func (d *ritualDrive) LoadedIdentity() (executor.LoadedIdentity, error) {
	identity, err := d.DirectoryDrive.LoadedIdentity()
	if err == nil && d.mediumSerial != "" {
		identity.MediumSerial = d.mediumSerial
	}
	return identity, err
}

func (d *ritualDrive) EncryptionActive() (bool, error) {
	d.record("encryption")
	d.mu.Lock()
	d.encryptionChecks++
	check := d.encryptionChecks
	inactiveOn := d.inactiveOnEncryptionCheck
	d.mu.Unlock()
	if inactiveOn != 0 && check == inactiveOn {
		return false, nil
	}
	return d.DirectoryDrive.EncryptionActive()
}

func (d *ritualDrive) Capacity() (total, free int64, err error) {
	d.mu.Lock()
	d.capacityCalls++
	call := d.capacityCalls
	unmountOn := d.unmountOnCapacityCall
	fixed := d.fixedCapacity
	d.mu.Unlock()
	if call == unmountOn {
		if err := d.DirectoryDrive.SetMounted("ABC123L6", false); err != nil {
			return 0, 0, err
		}
	}
	if fixed {
		return 1 << 20, 1 << 20, nil
	}
	return d.DirectoryDrive.Capacity()
}

func (d *ritualDrive) Format(tapeID tape.ID) error {
	d.record("format")
	if err := d.DirectoryDrive.Format(tapeID); err != nil {
		return err
	}
	return d.DirectoryDrive.SetMounted(tapeID, true)
}

func (d *ritualDrive) StampIdentity(volumeID volume.ID, tapeID tape.ID) error {
	d.record("stamp")
	return d.DirectoryDrive.StampIdentity(volumeID, tapeID)
}

func (d *ritualDrive) Sync() error {
	d.record("sync")
	return d.DirectoryDrive.Sync()
}

func (d *ritualDrive) record(call string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, call)
}

func (d *ritualDrive) callSequence() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.calls)
}

func (d *ritualDrive) countCall(want string) int {
	count := 0
	for _, call := range d.callSequence() {
		if call == want {
			count++
		}
	}
	return count
}

func isSubsequence(got, want []string) bool {
	index := 0
	for _, value := range got {
		if index < len(want) && value == want[index] {
			index++
		}
	}
	return index == len(want)
}

func assertExactPlanFailure(
	t *testing.T,
	session backupplan.Session,
	want string,
) {
	t.Helper()
	assertEvent(t, session.Events, backupplan.EventPlanFailed, func(event backupplan.Event) bool {
		return event.Detail == want
	})
}

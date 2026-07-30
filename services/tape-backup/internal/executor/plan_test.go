package executor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/executor"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapecatalog"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
)

func TestRunSessionFlushCadenceBatchesOnlyAtSnapshotBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		cadence   int
		wantSyncs int
	}{
		{name: "per snapshot default", cadence: 1, wantSyncs: 2},
		{name: "two snapshots", cadence: 2, wantSyncs: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSessionFixture(t)
			first := writeSeries(t, fixture.libraryRoot, "Series A", "tvdb:a", 1)
			second := writeSeries(t, fixture.libraryRoot, "Series B", "tvdb:b", 1)
			fixture.plan = fillPlan(
				firstPlanID,
				firstVolume,
				"ABC123L6",
				nil,
				[]backupplan.Action{first, second},
			)
			fixture.promote(t)
			drive := &countingDrive{Drive: fixture.drive}
			handler := fixture.backupHandler(t)

			err := executor.RunSession(
				t.Context(),
				drive,
				[]backupplan.Plan{fixture.plan},
				fixture.journal,
				handler,
				executor.SessionOptions{
					PollInterval:    time.Millisecond,
					IdleTimeout:     50 * time.Millisecond,
					StateRoot:       fixture.stateRoot,
					LibraryRoot:     fixture.libraryRoot,
					FreeSpaceMargin: 0,
					FlushCadence:    test.cadence,
				},
			)
			if closeErr := fixture.journal.Close(); closeErr != nil {
				t.Fatalf("Close journal error = %v", closeErr)
			}
			if err != nil {
				t.Fatalf("RunSession error = %v", err)
			}
			if drive.syncs != test.wantSyncs {
				t.Fatalf("Drive.Sync calls = %d, want %d", drive.syncs, test.wantSyncs)
			}
		})
	}
}

func TestRunSessionEntryGuardRefusesMalformedCatalogBeforeSweep(t *testing.T) {
	fixture := newSessionFixture(t)
	markerless := snapshotName(t, "tvdb:partial", 1)
	writeFile(t, filepath.Join(
		tapevolume.SnapshotsDir(fixture.cartridgeRoot),
		markerless,
		"tree",
		"partial.mkv",
	), []byte("partial"))
	volumeDir, err := tapecatalog.VolumeDir(fixture.stateRoot, firstVolume)
	if err != nil {
		t.Fatalf("VolumeDir error = %v", err)
	}
	if err := os.Remove(filepath.Join(volumeDir, "observed.json")); err != nil {
		t.Fatalf("Remove observed error = %v", err)
	}
	fixture.plan = fillPlan(firstPlanID, firstVolume, "ABC123L6", nil, nil)
	fixture.promote(t)

	if err := fixture.run(t, nil); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	if _, err := os.Lstat(filepath.Join(
		tapevolume.SnapshotsDir(fixture.cartridgeRoot),
		markerless,
	)); err != nil {
		t.Fatalf("entry-guard refusal swept markerless entry: %v", err)
	}
	assertPlanDiscarded(t, fixture.stateRoot, firstPlanID)
}

func TestRunSessionRegistryRefusesConcurrentSession(t *testing.T) {
	first := newSessionFixture(t)
	action := writeSeries(t, first.libraryRoot, "Series A", "tvdb:a", 1)
	first.plan = fillPlan(
		firstPlanID,
		firstVolume,
		"ABC123L6",
		nil,
		[]backupplan.Action{action},
	)
	first.promote(t)
	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- executor.RunSession(
			t.Context(),
			first.drive,
			[]backupplan.Plan{first.plan},
			first.journal,
			func(context.Context, executor.BackupActionRequest) error {
				close(started)
				<-release
				return errors.New("stop first session")
			},
			executor.SessionOptions{
				PollInterval:    time.Millisecond,
				IdleTimeout:     time.Second,
				StateRoot:       first.stateRoot,
				LibraryRoot:     first.libraryRoot,
				FreeSpaceMargin: 0,
				FlushCadence:    1,
			},
		)
	}()
	<-started

	second := newSessionFixture(t)
	second.plan = fillPlan(secondPlanID, firstVolume, "ABC123L6", nil, nil)
	second.promote(t)
	err := executor.RunSession(
		t.Context(),
		second.drive,
		[]backupplan.Plan{second.plan},
		second.journal,
		second.backupHandler(t),
		executor.SessionOptions{
			PollInterval:    time.Millisecond,
			IdleTimeout:     50 * time.Millisecond,
			StateRoot:       second.stateRoot,
			LibraryRoot:     second.libraryRoot,
			FreeSpaceMargin: 0,
			FlushCadence:    1,
		},
	)
	if !errors.Is(err, executor.ErrSessionActive) {
		t.Fatalf("concurrent RunSession error = %v, want ErrSessionActive", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first RunSession error = %v", err)
	}
	if err := first.journal.Close(); err != nil {
		t.Fatalf("Close first journal error = %v", err)
	}
	if err := second.journal.Close(); err != nil {
		t.Fatalf("Close second journal error = %v", err)
	}
}

func TestExecutePreparedPlanValidationUsesExactErrors(t *testing.T) {
	fixture := newCopyFixture(t, nil)
	tests := []struct {
		name    string
		mutate  func(*executor.PreparedPlan)
		cadence int
		want    string
	}{
		{
			name: "missing state root",
			mutate: func(prepared *executor.PreparedPlan) {
				prepared.StateRoot = ""
			},
			cadence: 1,
			want:    "executor: prepared plan state root is required",
		},
		{
			name:    "zero cadence",
			mutate:  func(*executor.PreparedPlan) {},
			cadence: 0,
			want:    "executor: flush cadence must be at least 1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prepared := fixture.prepared
			test.mutate(&prepared)
			err := executor.ExecutePreparedPlan(
				t.Context(),
				prepared,
				0,
				test.cadence,
				fixture.handler(t, executor.OpenDestination),
			)
			assertExactError(t, err, test.want)
		})
	}
}

type countingDrive struct {
	executor.Drive
	syncs int
}

func (d *countingDrive) Sync() error {
	d.syncs++
	return d.Drive.Sync()
}

var _ executor.Drive = (*countingDrive)(nil)

func TestRunSessionMountsMatchingCartridgeWithoutOperatorStep(t *testing.T) {
	fixture := newSessionFixture(t)
	fixture.plan = fillPlan(firstPlanID, firstVolume, "ABC123L6", nil, nil)
	fixture.promote(t)
	if err := fixture.drive.SetMounted("ABC123L6", false); err != nil {
		t.Fatalf("SetMounted false error = %v", err)
	}

	if err := fixture.run(t, nil); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}
	if fixture.drive.IsMounted("ABC123L6") {
		t.Fatal("fill session left the matching cartridge mounted")
	}
	if _, err := backupplan.ReadDone(fixture.stateRoot, firstPlanID); err != nil {
		t.Fatalf("ReadDone() error = %v", err)
	}
}

func TestRunSessionIdleTimeoutRetiresPendingPlan(t *testing.T) {
	fixture := newSessionFixture(t)
	fixture.plan = fillPlan(firstPlanID, firstVolume, "ABC123L6", nil, nil)
	fixture.promote(t)
	fixture.drive.ClearLoaded()

	if err := fixture.run(t, nil); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}
	assertPlanDiscarded(t, fixture.stateRoot, firstPlanID)
	session := fixture.readSession(t)
	if session.Terminal == nil || session.Terminal.Reason != "idle_timeout" {
		t.Fatalf("terminal = %#v, want idle_timeout", session.Terminal)
	}
}

func TestRunSessionCancellationRetiresPendingPlan(t *testing.T) {
	fixture := newSessionFixture(t)
	fixture.plan = fillPlan(firstPlanID, firstVolume, "ABC123L6", nil, nil)
	fixture.promote(t)
	fixture.drive.ClearLoaded()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := executor.RunSession(
		ctx,
		fixture.drive,
		[]backupplan.Plan{fixture.plan},
		fixture.journal,
		fixture.backupHandler(t),
		executor.SessionOptions{
			PollInterval:    time.Millisecond,
			IdleTimeout:     time.Second,
			StateRoot:       fixture.stateRoot,
			LibraryRoot:     fixture.libraryRoot,
			FreeSpaceMargin: 0,
			FlushCadence:    1,
		},
	)
	if closeErr := fixture.journal.Close(); closeErr != nil {
		t.Fatalf("Close journal error = %v", closeErr)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunSession error = %v, want context.Canceled", err)
	}
	assertPlanDiscarded(t, fixture.stateRoot, firstPlanID)
}

func TestRunSessionKeySetFailureWritesNothingAndRetires(t *testing.T) {
	stateRoot := t.TempDir()
	libraryRoot := t.TempDir()
	cartridgeRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, tape.ID("ABC123L6"), firstVolume)
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             cartridgeRoot,
		Mounted:          true,
		IdentityState:    executor.LoadedIdentityIdentified,
		MediumSerial:     "MAM-ABC123-0001",
		EncryptionActive: false,
		Capacity:         1 << 20,
	}}, "ABC123L6")
	drive.SetFaults(executor.DriveFaults{
		SetEncryptionKey: errors.New("injected key programming failure"),
	})
	installCatalogVolume(
		t,
		stateRoot,
		cartridgeRoot,
		firstVolume,
		"ABC123L6",
		1<<20,
		1<<20,
	)
	plan := fillPlan(firstPlanID, firstVolume, "ABC123L6", nil, nil)
	if err := backupplan.Draft(stateRoot, plan); err != nil {
		t.Fatalf("Draft error = %v", err)
	}
	if err := backupplan.Approve(stateRoot, plan.PlanID); err != nil {
		t.Fatalf("Approve error = %v", err)
	}
	journal := newSessionJournal(t, stateRoot, []string{plan.PlanID})

	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{plan},
		journal,
		func(context.Context, executor.BackupActionRequest) error {
			t.Fatal("backup handler called after key setup failure")
			return nil
		},
		executor.SessionOptions{
			PollInterval:    time.Millisecond,
			IdleTimeout:     50 * time.Millisecond,
			StateRoot:       stateRoot,
			LibraryRoot:     libraryRoot,
			FreeSpaceMargin: 0,
			FlushCadence:    1,
		},
	)
	if closeErr := journal.Close(); closeErr != nil {
		t.Fatalf("Close journal error = %v", closeErr)
	}
	if err != nil {
		t.Fatalf("RunSession error = %v", err)
	}
	assertPlanDiscarded(t, stateRoot, plan.PlanID)
	session, err := backupplan.ReadSession(stateRoot, sessionID)
	if err != nil {
		t.Fatalf("ReadSession() error = %v", err)
	}
	const want = "executor: encryption_key_setup_failed: injected key programming failure"
	assertExactPlanFailure(t, session, want)
}

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/executor"
	"github.com/wyvernzora/kura/services/tape-backup/internal/planner"
	"github.com/wyvernzora/kura/services/tape-backup/internal/snapshotname"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapecatalog"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapemanifest"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

const (
	testTape   tape.ID   = "ABC123L6"
	secondTape tape.ID   = "DEF456L6"
	testVolume volume.ID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
)

func TestRunExecutesValidatedBeliefDraftVerbatimAfterPendingChanges(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
	f.addSeries(t, "A", "tvdb:a", 8)
	consult, err := f.service.Consult(nil)
	assertNoError(t, err)
	if got := backupRefs(consult.Drafts[0]); !reflect.DeepEqual(got, []string{"tvdb:a"}) {
		t.Fatalf("consult backups = %v, want [tvdb:a]", got)
	}

	f.addSeries(t, "D", "tvdb:d", 64)
	run, err := f.service.Run(t.Context(), PlanRequest{})
	assertNoError(t, err)
	if run.Classification != "fill" || run.Plan.PlanID != consult.Drafts[0].PlanID {
		t.Fatalf("Run() = %+v, want exact consulted fill plan", run)
	}

	catalog, err := planner.ReadCatalogSnapshot(f.stateRoot)
	assertNoError(t, err)
	got := make([]string, 0)
	for _, snapshot := range catalog.Volumes[0].Snapshots {
		got = append(got, snapshot.MetadataRef)
	}
	if !reflect.DeepEqual(got, []string{"tvdb:a"}) {
		t.Fatalf("executed backups = %v, want verbatim [tvdb:a]", got)
	}
}

func TestRunValidatedBeliefDraftSurvivesDebrisAppearingAfterConsult(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
	f.addSeries(t, "A", "tvdb:a", 8)
	consult, err := f.service.Consult(nil)
	assertNoError(t, err)
	f.addSeries(t, "B", "tvdb:b", 64)
	debrisName := writeMarked(t, f.driveRoot(t), "tvdb:b", 1)

	run, err := f.service.Run(t.Context(), PlanRequest{})
	assertNoError(t, err)
	if run.Classification != "fill" || run.Plan.PlanID != consult.Drafts[0].PlanID {
		t.Fatalf("Run() = %+v, want exact consulted fill plan", run)
	}

	catalog, err := planner.ReadCatalogSnapshot(f.stateRoot)
	assertNoError(t, err)
	if got := catalogSnapshotNames(catalog, testVolume); !reflect.DeepEqual(
		got,
		[]string{mustSnapshotName(t, "tvdb:a", 1)},
	) {
		t.Fatalf("catalog snapshots = %v, want only fresh A", got)
	}
	assertPlanDiscarded(t, f.stateRoot, consult.Drafts[0].PlanID, false)
	session, err := backupplan.ReadSession(f.stateRoot, f.lastSessionID())
	assertNoError(t, err)
	if !sessionLoggedExtra(session, debrisName) {
		t.Fatalf("session did not log debris %q", debrisName)
	}
}

func TestConsultDampsAndPreservesInitDraft(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
	f.addSeries(t, "A", "tvdb:a", 8)
	f.addSeries(t, "B", "tvdb:b", 16)
	first, err := f.service.Consult(nil)
	assertNoError(t, err)
	second, err := f.service.Consult(nil)
	assertNoError(t, err)
	if !reflect.DeepEqual(first.Drafts[0].Target, second.Drafts[0].Target) ||
		!reflect.DeepEqual(first.Drafts[0].Actions, second.Drafts[0].Actions) {
		t.Fatalf("re-consult changed target/actions")
	}
	for _, action := range first.Drafts[0].Actions {
		switch action.Type {
		case backupplan.ActionAssertVolume,
			backupplan.ActionAssertInventory,
			backupplan.ActionBackup:
		default:
			t.Fatalf("consult draft contains destructive/deferred action %q", action.Type)
		}
	}

	init := f.initDraft(t, secondTape, "SERIAL-2", "tvdb:held")
	assertNoError(t, backupplan.Draft(f.stateRoot, init))
	third, err := f.service.Consult(nil)
	assertNoError(t, err)
	preserved, err := backupplan.ReadDraft(f.stateRoot, init.PlanID)
	assertNoError(t, err)
	if preserved.PlanID != init.PlanID {
		t.Fatalf("preserved init planID = %q, want %q", preserved.PlanID, init.PlanID)
	}
	if len(third.Report.InitPlansAwaitingApproval) != 1 ||
		len(third.Report.InitPlansAwaitingApproval[0].Claimed) != 1 {
		t.Fatalf("init status = %+v, want one held claim", third.Report.InitPlansAwaitingApproval)
	}
}

func TestConsultAndStatusRemainAvailableWhileDriveIsOffline(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
	f.addSeries(t, "A", "tvdb:a", 8)
	f.drive.SetFaults(executor.DriveFaults{Open: os.ErrNotExist})

	result, err := f.service.Consult(nil)
	assertNoError(t, err)
	if len(result.Drafts) != 1 {
		t.Fatalf("consult drafts = %d, want 1", len(result.Drafts))
	}
	status, err := f.service.Status()
	assertNoError(t, err)
	if status.Drive != (DriveStatus{
		State:   "drive_offline",
		Message: "drive offline",
	}) {
		t.Fatalf("drive status = %#v, want exact offline refusal", status.Drive)
	}
}

func TestServiceMutexSerializesPromotionAgainstConsultSweep(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
	f.addSeries(t, "A", "tvdb:a", 8)
	_, err := f.service.Consult(nil)
	assertNoError(t, err)

	entered := make(chan struct{})
	release := make(chan struct{})
	f.service.beforePromote = func() {
		close(entered)
		<-release
	}
	runDone := make(chan error, 1)
	go func() {
		_, runErr := f.service.Run(context.Background(), PlanRequest{})
		runDone <- runErr
	}()
	<-entered
	consultDone := make(chan error, 1)
	go func() {
		_, err := f.service.Consult(nil)
		consultDone <- err
	}()
	var consultErr error
	consultCrossed := false
	select {
	case err := <-consultDone:
		consultErr = err
		consultCrossed = true
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	assertNoError(t, <-runDone)
	if !consultCrossed {
		consultErr = <-consultDone
	}
	assertNoError(t, consultErr)
}

func TestRunPanicReleasesLiveSessionAndJournal(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
	f.addSeries(t, "A", "tvdb:a", 8)
	_, err := f.service.Consult(nil)
	assertNoError(t, err)
	f.service.deps.Backup = func(context.Context, executor.BackupActionRequest) error {
		panic("faulting backup handler")
	}

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		_, _ = f.service.Run(t.Context(), PlanRequest{})
	}()
	if recovered != "faulting backup handler" {
		t.Fatalf("recovered panic = %v, want %q", recovered, "faulting backup handler")
	}

	statusResult := make(chan StatusResult, 1)
	statusErr := make(chan error, 1)
	go func() {
		status, statusCallErr := f.service.Status()
		statusResult <- status
		statusErr <- statusCallErr
	}()
	select {
	case status := <-statusResult:
		assertNoError(t, <-statusErr)
		if len(status.LiveSessions) != 0 {
			t.Fatalf("live sessions = %+v, want empty after panic", status.LiveSessions)
		}
	case <-time.After(time.Second):
		t.Fatal("Status() blocked after Run panic")
	}

	journal, err := backupplan.OpenSession(f.stateRoot, f.lastSessionID())
	assertNoError(t, err)
	assertNoError(t, journal.Close())
}

func TestTargetedPlanKeepsCollidingDebrisPending(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
	f.addSeries(t, "B", "tvdb:b", 64)
	debrisName := writeMarked(t, f.driveRoot(t), "tvdb:b", 1)

	result, err := f.service.Plan(PlanRequest{})
	assertNoError(t, err)
	if got := backupRefs(result.Plan); !reflect.DeepEqual(got, []string{"tvdb:b"}) {
		t.Fatalf("backup refs = %v, want debris series B pending", got)
	}
	if !reflect.DeepEqual(result.Debris, []string{debrisName}) {
		t.Fatalf("debris = %v, want [%s]", result.Debris, debrisName)
	}
}

func TestTargetedPlanDoesNotCreditDebrisBytes(t *testing.T) {
	cartridge := identifiedCartridge(t, testTape, testVolume, true)
	cartridge.Capacity = 512
	f := newFixture(t, cartridge)
	f.addSeries(t, "A", "tvdb:a", 8)
	f.addSeries(t, "B", "tvdb:b", 1<<20)
	debrisName := writeMarked(t, f.driveRoot(t), "tvdb:b", 1)

	result, err := f.service.Plan(PlanRequest{})
	assertNoError(t, err)
	if got := backupRefs(result.Plan); !reflect.DeepEqual(got, []string{"tvdb:a"}) {
		t.Fatalf("backup refs = %v, want only fitting A", got)
	}
	if !reflect.DeepEqual(result.Debris, []string{debrisName}) {
		t.Fatalf("debris = %v, want [%s]", result.Debris, debrisName)
	}
}

func TestTargetedPlanToleratesProtectedElsewhereDebrisWithoutRepending(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
	f.addSeries(t, "B", "tvdb:b", 64)
	installProtectedSnapshot(t, f.stateRoot, secondTape, "tvdb:b", 1)
	debrisName := writeMarked(t, f.driveRoot(t), "tvdb:b", 1)

	result, err := f.service.Plan(PlanRequest{})
	assertNoError(t, err)
	if result.Classification != "fill" || result.Plan.PlanID != "" {
		t.Fatalf("Plan() = %+v, want no-work fill", result)
	}
	if !reflect.DeepEqual(result.Debris, []string{debrisName}) {
		t.Fatalf("debris = %v, want [%s]", result.Debris, debrisName)
	}
	consult, err := f.service.Consult(nil)
	assertNoError(t, err)
	if len(consult.Report.Pending) != 0 {
		t.Fatalf("pending = %+v, want protected series absent", consult.Report.Pending)
	}
}

func TestRunMootsMismatchedBeliefDraftBeforeStubRefusal(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
	f.addSeries(t, "A", "tvdb:a", 8)
	consult, err := f.service.Consult(nil)
	assertNoError(t, err)
	foreignName := writeMarked(t, f.driveRoot(t), "tvdb:foreign", 1)

	_, err = f.service.Run(t.Context(), PlanRequest{})
	want := "divergence_deferred: tape's contents diverge from the catalog " +
		"(missing=[] extra=[" + foreignName + "]); divergence handling is deferred"
	assertError(t, err, want)
	assertPlanDiscarded(t, f.stateRoot, consult.Drafts[0].PlanID, true)
}

func TestTargetedPlanningRetiresStaleReadyPlan(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
	f.addSeries(t, "A", "tvdb:a", 8)
	consult, err := f.service.Consult(nil)
	assertNoError(t, err)
	stale := consult.Drafts[0]
	assertNoError(t, backupplan.Approve(f.stateRoot, stale.PlanID))

	result, err := f.service.Plan(PlanRequest{})
	assertNoError(t, err)
	if result.Plan.PlanID == stale.PlanID {
		t.Fatalf("targeted plan reused stale ready plan %s", stale.PlanID)
	}
	assertPlanDiscarded(t, f.stateRoot, stale.PlanID, true)
}

func TestStubOperationsRefuseWithExactCodes(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
	tests := []struct {
		operation string
		want      string
	}{
		{"compaction", "compaction_deferred: compaction is deferred"},
		{"sync", "sync_deferred: sync-to-catalog is deferred"},
		{"recovery", "recovery_deferred: recovery is deferred"},
	}
	for _, test := range tests {
		t.Run(test.operation, func(t *testing.T) {
			_, err := f.service.Plan(PlanRequest{Operation: test.operation})
			assertError(t, err, test.want)
			_, err = f.service.Run(t.Context(), PlanRequest{Operation: test.operation})
			assertError(t, err, test.want)
		})
	}
}

func TestDecisionTreeRefusesIdentityAdoptionAndDivergence(t *testing.T) {
	t.Run("identity", func(t *testing.T) {
		f := newFixture(t, unidentifiedCartridge(t, testTape))
		_, err := f.service.Plan(PlanRequest{})
		want := "identity_unconfirmed: unidentified media requires the declared cartridge label"
		assertError(t, err, want)
		_, err = f.service.Run(t.Context(), PlanRequest{})
		assertError(t, err, want)
	})
	t.Run("adoption", func(t *testing.T) {
		const unknown volume.ID = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
		cartridge := identifiedCartridge(t, testTape, unknown, false)
		writeMarked(t, cartridge.Root, "tvdb:foreign", 1)
		f := newFixture(t, cartridge)
		_, err := f.service.Plan(PlanRequest{})
		want := "adoption_deferred: cartridge carries volume " + string(unknown) +
			", not in the catalog, with 1 committed snapshots; adoption/reformat of non-blank tapes is deferred"
		assertError(t, err, want)
		_, err = f.service.Run(t.Context(), PlanRequest{})
		assertError(t, err, want)
	})
	t.Run("divergence", func(t *testing.T) {
		cartridge := identifiedCartridge(t, testTape, testVolume, true)
		writeMarked(t, cartridge.Root, "tvdb:foreign", 1)
		f := newFixture(t, cartridge)
		_, err := f.service.Plan(PlanRequest{})
		name, parseErr := snapshotname.Format("tvdb:foreign", 1)
		assertNoError(t, parseErr)
		want := "divergence_deferred: tape's contents diverge from the catalog " +
			"(missing=[] extra=[" + name + "]); divergence handling is deferred"
		assertError(t, err, want)
		_, err = f.service.Run(t.Context(), PlanRequest{})
		assertError(t, err, want)
	})
}

func TestStatusSurfacesDraftInitPlanWithoutMutatingIt(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
	f.addSeries(t, "A", "tvdb:a", 8)
	init := f.initDraft(t, secondTape, "SERIAL-2", "tvdb:held")
	assertNoError(t, backupplan.Draft(f.stateRoot, init))
	_, err := f.service.Consult(nil)
	assertNoError(t, err)
	status, err := f.service.Status()
	assertNoError(t, err)
	if len(status.InitDrafts) != 1 ||
		status.InitDrafts[0].PlanID != init.PlanID ||
		len(status.InitDrafts[0].Claimed) != 1 {
		t.Fatalf("Status().InitDrafts = %+v", status.InitDrafts)
	}
	if status.Consult == nil || status.Consult.SchemaVersion != reportSchemaVersion {
		t.Fatalf("Status().Consult = %+v, want persisted report", status.Consult)
	}
	_, err = backupplan.ReadDraft(f.stateRoot, init.PlanID)
	assertNoError(t, err)
}

func TestTargetedPlanLifecycleKeepsFillsEphemeralAndInitCeremonial(t *testing.T) {
	t.Run("fill is ephemeral", func(t *testing.T) {
		f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
		f.addSeries(t, "A", "tvdb:a", 8)
		result, err := f.service.Plan(PlanRequest{})
		assertNoError(t, err)
		if result.Classification != "fill" || result.Persisted {
			t.Fatalf("Plan() = %+v, want ephemeral fill", result)
		}
		drafts, err := backupplan.ListDraft(f.stateRoot)
		assertNoError(t, err)
		if len(drafts) != 0 {
			t.Fatalf("draft plans = %d, want 0", len(drafts))
		}
	})

	t.Run("init awaits approve and can be discarded", func(t *testing.T) {
		f := newFixture(t, unidentifiedCartridge(t, testTape))
		f.addSeries(t, "A", "tvdb:a", 8)
		result, err := f.service.Plan(PlanRequest{TapeID: testTape})
		assertNoError(t, err)
		if result.Classification != "init" || !result.Persisted {
			t.Fatalf("Plan() = %+v, want persisted init", result)
		}
		assertNoError(t, f.service.Approve(result.Plan.PlanID))
		ready, err := backupplan.ListReady(f.stateRoot)
		assertNoError(t, err)
		if len(ready) != 1 {
			t.Fatalf("ready plans = %d, want 1", len(ready))
		}
		assertNoError(t, f.service.Discard(result.Plan.PlanID))
		ready, err = backupplan.ListReady(f.stateRoot)
		assertNoError(t, err)
		if len(ready) != 0 {
			t.Fatalf("ready plans after discard = %d, want 0", len(ready))
		}
	})
}

func TestApproveNonInitDraftIsClassifiable(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, true))
	f.addSeries(t, "A", "tvdb:a", 8)
	consult, err := f.service.Consult(nil)
	assertNoError(t, err)

	err = f.service.Approve(consult.Drafts[0].PlanID)
	const want = "tape service: only init drafts can be approved"
	assertError(t, err, want)
	if !errors.Is(err, ErrApprovalNotAllowed) {
		t.Fatalf("Approve() error = %v, want ErrApprovalNotAllowed", err)
	}
}

func TestTargetedInitPlanReturnsObservedMediaAttestation(t *testing.T) {
	f := newFixture(t, identifiedCartridge(t, testTape, testVolume, false))
	f.addSeries(t, "A", "tvdb:a", 8)
	total, free, err := f.drive.Capacity()
	assertNoError(t, err)

	result, err := f.service.Plan(PlanRequest{})
	assertNoError(t, err)

	want := PlanTarget{
		TapeID:        testTape,
		MediumSerial:  "SERIAL-UNKNOWN",
		VolumeID:      testVolume,
		UsedBytes:     total - free,
		FreeBytes:     free,
		CapacityBytes: total,
	}
	if result.Classification != "init" || result.Target != want {
		t.Fatalf("Plan() = %+v, want init target %+v", result, want)
	}
}

type fixture struct {
	service     *Service
	drive       *executor.DirectoryDrive
	stateRoot   string
	libraryRoot string
	ids         []string
	issued      []string
}

func newFixture(t *testing.T, cartridge executor.DirectoryCartridge) *fixture {
	t.Helper()
	stateRoot := t.TempDir()
	libraryRoot := t.TempDir()
	drive, err := executor.NewDirectoryDrive([]executor.DirectoryCartridge{cartridge})
	assertNoError(t, err)
	assertNoError(t, drive.SelectLoaded(cartridge.TapeID))
	if cartridge.IdentityState == executor.LoadedIdentityIdentified &&
		cartridge.TapeID == testTape &&
		cartridge.Root != "" &&
		cartridge.MediumSerial == "SERIAL-KNOWN" {
		header, readErr := os.ReadFile(tapevolume.VolumeFile(cartridge.Root))
		assertNoError(t, readErr)
		assertNoError(t, tapecatalog.InstallVolume(
			stateRoot,
			testVolume,
			header,
			tapecatalog.Observed{
				TapeID:        testTape,
				ObservedAt:    time.Unix(1, 0).UTC(),
				CapacityBytes: cartridge.Capacity,
				FreeBytes:     cartridge.Capacity,
			},
		))
	}
	handler, err := executor.NewBackupActionHandler(nil, executor.OpenDestination, drive)
	assertNoError(t, err)
	f := &fixture{
		drive: drive, stateRoot: stateRoot, libraryRoot: libraryRoot,
		ids: []string{
			"01ARZ3NDEKTSV4RRFFQ69G5FAX",
			"01ARZ3NDEKTSV4RRFFQ69G5FAY",
			"01ARZ3NDEKTSV4RRFFQ69G5FAZ",
			"01ARZ3NDEKTSV4RRFFQ69G5FB0",
			"01ARZ3NDEKTSV4RRFFQ69G5FB1",
			"01ARZ3NDEKTSV4RRFFQ69G5FB2",
			"01ARZ3NDEKTSV4RRFFQ69G5FB3",
			"01ARZ3NDEKTSV4RRFFQ69G5FB4",
		},
	}
	service, err := New(Deps{
		StateRoot: stateRoot, LibraryRoot: libraryRoot, Drive: drive, Backup: handler,
		Creator:         backupplan.Creator{Version: "test", Host: "test-host"},
		FreeSpaceMargin: 0, FlushCadence: 1,
		PollInterval: time.Millisecond, IdleTimeout: time.Second,
		Now: func() time.Time { return time.Unix(100, 0).UTC() },
		NewID: func() string {
			id := f.ids[0]
			f.ids = f.ids[1:]
			f.issued = append(f.issued, id)
			return id
		},
	})
	assertNoError(t, err)
	f.service = service
	return f
}

func identifiedCartridge(
	t *testing.T,
	tapeID tape.ID,
	volumeID volume.ID,
	known bool,
) executor.DirectoryCartridge {
	t.Helper()
	root := t.TempDir()
	assertNoError(t, tapevolume.Write(root, tapevolume.Volume{
		VolumeID: volumeID, TapeID: tapeID, CreatedAt: time.Unix(1, 0).UTC(),
	}))
	assertNoError(t, os.MkdirAll(tapevolume.SnapshotsDir(root), 0o775))
	serial := "SERIAL-UNKNOWN"
	if known {
		serial = "SERIAL-KNOWN"
	}
	return executor.DirectoryCartridge{
		TapeID: tapeID, Root: root, Mounted: true,
		IdentityState: executor.LoadedIdentityIdentified,
		MediumSerial:  serial, EncryptionActive: true, Capacity: 1 << 30,
	}
}

func unidentifiedCartridge(t *testing.T, tapeID tape.ID) executor.DirectoryCartridge {
	t.Helper()
	return executor.DirectoryCartridge{
		TapeID: tapeID, Root: t.TempDir(), Mounted: true,
		IdentityState: executor.LoadedIdentityUnidentified,
		MediumSerial:  "SERIAL-1", EncryptionActive: true, Capacity: 1 << 30,
	}
}

func (f *fixture) addSeries(
	t *testing.T,
	dir, metadataRef string,
	payloadBytes int,
) {
	t.Helper()
	root := filepath.Join(f.libraryRoot, dir)
	assertNoError(t, os.MkdirAll(filepath.Join(root, ".kura"), 0o775))
	metadata := `{"schemaVersion":3,"generation":1,"metadataRef":"` +
		metadataRef + `","episodes":{},"stagedTrash":[],"stagedExtras":[]}`
	assertNoError(t, os.WriteFile(
		filepath.Join(root, ".kura", "series.json"),
		[]byte(metadata),
		0o664,
	))
	assertNoError(t, os.WriteFile(
		filepath.Join(root, "payload.mkv"),
		make([]byte, payloadBytes),
		0o664,
	))
}

func (f *fixture) initDraft(
	t *testing.T,
	tapeID tape.ID,
	serial, metadataRef string,
) backupplan.Plan {
	t.Helper()
	name, err := snapshotname.Format(metadataRef, 1)
	assertNoError(t, err)
	planID := f.ids[0]
	f.ids = f.ids[1:]
	return backupplan.Plan{
		PlanID: planID, CreatedAt: time.Unix(90, 0).UTC(),
		CreatedBy: backupplan.Creator{Version: "test", Host: "test-host"},
		Target:    backupplan.Target{TapeID: tapeID, MediumSerial: serial},
		Actions: []backupplan.Action{
			{Type: backupplan.ActionReformat},
			{Type: backupplan.ActionAdmit, VolumeID: "01ARZ3NDEKTSV4RRFFQ69G5FAV"},
			{
				Type: backupplan.ActionBackup, MetadataRef: metadataRef,
				RootPath: "A", Generation: 1,
				PayloadFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
			{Type: backupplan.ActionAssertInventory, Snapshots: []string{name}},
		},
	}
}

func writeMarked(t *testing.T, root, metadataRef string, generation int) string {
	t.Helper()
	name, err := snapshotname.Format(metadataRef, generation)
	assertNoError(t, err)
	dir := filepath.Join(tapevolume.SnapshotsDir(root), name)
	assertNoError(t, os.MkdirAll(dir, 0o775))
	assertNoError(t, os.WriteFile(filepath.Join(dir, "complete.json"), []byte("{}"), 0o664))
	return name
}

func backupRefs(plan backupplan.Plan) []string {
	refs := make([]string, 0)
	for _, action := range plan.Actions {
		if action.Type == backupplan.ActionBackup {
			refs = append(refs, action.MetadataRef)
		}
	}
	return refs
}

func mustSnapshotName(t *testing.T, metadataRef string, generation int) string {
	t.Helper()
	name, err := snapshotname.Format(metadataRef, generation)
	assertNoError(t, err)
	return name
}

func catalogSnapshotNames(
	catalog planner.CatalogSnapshot,
	volumeID volume.ID,
) []string {
	for _, catalogVolume := range catalog.Volumes {
		if catalogVolume.VolumeID != volumeID {
			continue
		}
		names := make([]string, 0, len(catalogVolume.Snapshots))
		for _, snapshot := range catalogVolume.Snapshots {
			name, _ := snapshotname.Format(snapshot.MetadataRef, snapshot.Generation)
			names = append(names, name)
		}
		slices.Sort(names)
		return names
	}
	return []string{}
}

func (f *fixture) driveRoot(t *testing.T) string {
	t.Helper()
	identity, err := f.drive.LoadedIdentity()
	assertNoError(t, err)
	return identity.Cartridge.Root
}

func (f *fixture) lastSessionID() string {
	if len(f.issued) == 0 {
		return ""
	}
	return f.issued[len(f.issued)-1]
}

func sessionLoggedExtra(session backupplan.Session, name string) bool {
	for _, event := range session.Events {
		if event.Type == backupplan.EventDivergenceChecked &&
			slices.Contains(event.ExtraSnapshots, name) {
			return true
		}
	}
	return false
}

func assertPlanDiscarded(
	t *testing.T,
	stateRoot, planID string,
	wantDiscarded bool,
) {
	t.Helper()
	err := backupplan.Discard(stateRoot, planID)
	if !wantDiscarded {
		want := "backupplan: discard " + planID + ": done plan cannot be discarded"
		assertError(t, err, want)
		return
	}
	want := "backupplan: discard " + planID + ": plan is already discarded"
	assertError(t, err, want)
}

func installProtectedSnapshot(
	t *testing.T,
	stateRoot string,
	tapeID tape.ID,
	metadataRef string,
	generation int,
) {
	t.Helper()
	const protectedVolume volume.ID = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	root := t.TempDir()
	assertNoError(t, tapevolume.Write(root, tapevolume.Volume{
		VolumeID:  protectedVolume,
		TapeID:    tapeID,
		CreatedAt: time.Unix(1, 0).UTC(),
	}))
	header, err := os.ReadFile(tapevolume.VolumeFile(root))
	assertNoError(t, err)
	assertNoError(t, tapecatalog.InstallVolume(
		stateRoot,
		protectedVolume,
		header,
		tapecatalog.Observed{
			TapeID:        tapeID,
			ObservedAt:    time.Unix(1, 0).UTC(),
			CapacityBytes: 1 << 30,
			FreeBytes:     1 << 30,
		},
	))
	name := mustSnapshotName(t, metadataRef, generation)
	snapshotDir := filepath.Join(t.TempDir(), name)
	assertNoError(t, tapemanifest.Write(snapshotDir, tapemanifest.Manifest{
		MetadataRef: metadataRef,
		RootPath:    "B",
		Generation:  generation,
		CapturedAt:  time.Unix(1, 0).UTC(),
		WrittenBy:   tapemanifest.Writer{Version: "test", Host: "test-host"},
		TotalBytes:  1,
		Files: []tapemanifest.File{{
			Path:    "payload.mkv",
			Size:    1,
			ModTime: time.Unix(1, 0).UTC(),
			Hash:    "sha256:" + strings.Repeat("0", 64),
		}},
	}))
	assertNoError(t, tapemanifest.Commit(snapshotDir))
	manifest, err := os.ReadFile(filepath.Join(snapshotDir, "manifest.json"))
	assertNoError(t, err)
	complete, err := os.ReadFile(filepath.Join(snapshotDir, "complete.json"))
	assertNoError(t, err)
	assertNoError(t, tapecatalog.PutSnapshot(
		stateRoot,
		protectedVolume,
		name,
		manifest,
		complete,
	))
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
}

func assertError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

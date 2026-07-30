package executor_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/executor"
	"github.com/wyvernzora/kura/services/tape-backup/internal/fingerprint"
	"github.com/wyvernzora/kura/services/tape-backup/internal/planner"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapecatalog"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapemanifest"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

const (
	firstPlanID  = "01JAY7M2K8Q3V5N0X2W4Z6B8D1"
	secondPlanID = "01JAY8B4N1R6X8Q0Z2C4E6G8J3"
	sessionID    = "01JAY9C5P2S7Y9R1A3D5F7H9K4"
	firstVolume  = volume.ID("01J8ZQ7W5TWHA6R6J8X4QZ9Y7V")
	secondVolume = volume.ID("01J8ZQ7W5TWHA6R6J8X4QZ9Y7W")
)

func TestRunSessionFillCommitsAfterSyncAndCompletesPlan(t *testing.T) {
	fixture := newSessionFixture(t)
	action := writeSeries(t, fixture.libraryRoot, "Series A", "tvdb:a", 1)
	fixture.plan = fillPlan(firstPlanID, firstVolume, "ABC123L6", nil, []backupplan.Action{action})
	fixture.promote(t)

	if err := fixture.run(t, nil); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	name := snapshotName(t, action.MetadataRef, action.Generation)
	assertCatalogSnapshot(t, fixture.stateRoot, firstVolume, name, true)
	if !fixture.drive.WasSynced("ABC123L6") {
		t.Fatal("drive was not synced before completion")
	}
	if _, err := backupplan.ReadDone(fixture.stateRoot, firstPlanID); err != nil {
		t.Fatalf("ReadDone error = %v", err)
	}
	observed, err := tapecatalog.LoadObserved(fixture.stateRoot, firstVolume)
	if err != nil {
		t.Fatalf("LoadObserved error = %v", err)
	}
	if observed.FreeBytes >= observed.CapacityBytes {
		t.Fatalf(
			"observed capacity = %d/%d, want consumed bytes",
			observed.FreeBytes,
			observed.CapacityBytes,
		)
	}
	session := fixture.readSession(t)
	if session.Terminal == nil ||
		session.Terminal.Reason != "completed" ||
		session.Terminal.PlansCompleted != 1 {
		t.Fatalf("terminal = %#v, want completed plan", session.Terminal)
	}
}

func TestRunSessionSyncFailureAppendsNoCatalogRecordAndRetiresPlan(t *testing.T) {
	fixture := newSessionFixture(t)
	action := writeSeries(t, fixture.libraryRoot, "Series A", "tvdb:a", 1)
	fixture.plan = fillPlan(firstPlanID, firstVolume, "ABC123L6", nil, []backupplan.Action{action})
	fixture.promote(t)
	syncErr := errors.New("injected sync failure")
	fixture.drive.SetFaults(executor.DriveFaults{Sync: syncErr})

	if err := fixture.run(t, nil); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	name := snapshotName(t, action.MetadataRef, action.Generation)
	assertCatalogSnapshot(t, fixture.stateRoot, firstVolume, name, false)
	assertPlanDiscarded(t, fixture.stateRoot, firstPlanID)
	session := fixture.readSession(t)
	assertEvent(t, session.Events, backupplan.EventPlanFailed, func(event backupplan.Event) bool {
		return event.Reason == "halted" &&
			event.Detail == "executor: plan failed: executor: sync cartridge index: "+
				syncErr.Error()
	})
}

func TestRunSessionDebrisThroughRunIsLoggedNeverCatalogedAndDoesNotHalt(
	t *testing.T,
) {
	fixture := newSessionFixture(t)
	fill := writeSeries(t, fixture.libraryRoot, "Series A", "tvdb:a", 1)
	debrisAction := writeSeries(t, fixture.libraryRoot, "Series B", "tvdb:b", 4)
	debrisName, _, _ := writeCommittedSnapshot(
		t,
		fixture.cartridgeRoot,
		debrisAction,
		"old debris",
	)
	fixture.plan = fillPlan(firstPlanID, firstVolume, "ABC123L6", nil, []backupplan.Action{fill})
	fixture.promote(t)

	if err := fixture.run(t, nil); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	fillName := snapshotName(t, fill.MetadataRef, fill.Generation)
	assertCatalogSnapshot(t, fixture.stateRoot, firstVolume, fillName, true)
	assertCatalogSnapshot(t, fixture.stateRoot, firstVolume, debrisName, false)
	session := fixture.readSession(t)
	if session.Terminal == nil || session.Terminal.Reason != "completed" {
		t.Fatalf("terminal = %#v, want completed", session.Terminal)
	}
	matches := 0
	for _, event := range session.Events {
		if event.Type != backupplan.EventDivergenceChecked ||
			!slices.Contains(event.ExtraSnapshots, debrisName) {
			continue
		}
		matches++
	}
	if matches != 2 {
		t.Fatalf(
			"debris-bearing validation events = %d, want leading and trailing",
			matches,
		)
	}
}

func TestRunSessionCollidingDebrisIsOverwrittenAndOnlyFreshWriteIsCataloged(
	t *testing.T,
) {
	fixture := newSessionFixture(t)
	action := writeSeries(t, fixture.libraryRoot, "Series A", "tvdb:a", 1)
	name, oldManifest, _ := writeCommittedSnapshot(
		t,
		fixture.cartridgeRoot,
		action,
		"old debris",
	)
	fixture.plan = fillPlan(firstPlanID, firstVolume, "ABC123L6", nil, []backupplan.Action{action})
	fixture.promote(t)

	if err := fixture.run(t, nil); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	volumeDir, err := tapecatalog.VolumeDir(fixture.stateRoot, firstVolume)
	if err != nil {
		t.Fatalf("VolumeDir error = %v", err)
	}
	freshManifest, err := os.ReadFile(filepath.Join(
		tapevolume.SnapshotsDir(volumeDir),
		name,
		"manifest.json",
	))
	if err != nil {
		t.Fatalf("ReadFile catalog manifest error = %v", err)
	}
	if reflect.DeepEqual(freshManifest, oldManifest) {
		t.Fatal("catalog contains debris bytes instead of the fresh write")
	}
}

func TestRunSessionForeignMarkedContentRefusesAndRetiresBeforeCopy(t *testing.T) {
	fixture := newSessionFixture(t)
	fill := writeSeries(t, fixture.libraryRoot, "Series A", "tvdb:a", 1)
	foreign := backupAction("tvdb:foreign", "Foreign", 1, "sha256:foreign", 1)
	foreignName, _, _ := writeCommittedSnapshot(
		t,
		fixture.cartridgeRoot,
		foreign,
		"foreign",
	)
	fixture.plan = fillPlan(firstPlanID, firstVolume, "ABC123L6", nil, []backupplan.Action{fill})
	fixture.promote(t)
	called := false

	err := fixture.run(t, func(context.Context, executor.BackupActionRequest) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("RunSession error = %v", err)
	}
	if called {
		t.Fatal("backup handler was called for foreign marked content")
	}
	assertPlanDiscarded(t, fixture.stateRoot, firstPlanID)
	session := fixture.readSession(t)
	assertEvent(t, session.Events, backupplan.EventPlanFailed, func(event backupplan.Event) bool {
		return event.Reason == "halted" &&
			event.Detail == "executor: plan "+firstPlanID+
				" divergence_deferred: foreign marked snapshots: "+foreignName
	})
}

func TestRunSessionSweepRemovesConstructedGarbageAndContinues(t *testing.T) {
	fixture := newSessionFixture(t)
	committed := backupAction("tvdb:committed", "Committed", 1, "sha256:committed", 1)
	committedName, manifest, complete := writeCommittedSnapshot(
		t,
		fixture.cartridgeRoot,
		committed,
		"committed",
	)
	if err := tapecatalog.PutSnapshot(
		fixture.stateRoot,
		firstVolume,
		committedName,
		manifest,
		complete,
	); err != nil {
		t.Fatalf("PutSnapshot committed neighbor error = %v", err)
	}
	markerless := snapshotName(t, "tvdb:partial", 1)
	writeFile(t, filepath.Join(
		tapevolume.SnapshotsDir(fixture.cartridgeRoot),
		markerless,
		"tree",
		"partial.mkv",
	), []byte("partial"))
	writeFile(t, filepath.Join(
		tapevolume.SnapshotsDir(fixture.cartridgeRoot),
		"not-a-snapshot",
		"complete.json",
	), []byte("{}\n"))
	fixture.plan = fillPlan(
		firstPlanID,
		firstVolume,
		"ABC123L6",
		[]string{committedName},
		nil,
	)
	fixture.promote(t)

	if err := fixture.run(t, nil); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	for _, name := range []string{markerless, "not-a-snapshot"} {
		_, err := os.Lstat(filepath.Join(
			tapevolume.SnapshotsDir(fixture.cartridgeRoot),
			name,
		))
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Lstat swept %q error = %v, want file does not exist", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(
		tapevolume.SnapshotsDir(fixture.cartridgeRoot),
		committedName,
	)); err != nil {
		t.Fatalf("committed neighbor was swept: %v", err)
	}
	session := fixture.readSession(t)
	if got := countEvents(session.Events, backupplan.EventSnapshotSwept); got != 2 {
		t.Fatalf("snapshot_swept events = %d, want 2", got)
	}
}

func TestRunSessionCatalogMembershipSkipsWithoutReadingTapeManifest(t *testing.T) {
	fixture := newSessionFixture(t)
	action := writeSeries(t, fixture.libraryRoot, "Series A", "tvdb:a", 1)
	name, manifest, complete := writeCommittedSnapshot(
		t,
		fixture.cartridgeRoot,
		action,
		"cataloged",
	)
	if err := tapecatalog.PutSnapshot(
		fixture.stateRoot,
		firstVolume,
		name,
		manifest,
		complete,
	); err != nil {
		t.Fatalf("PutSnapshot error = %v", err)
	}
	writeFile(t, filepath.Join(
		tapevolume.SnapshotsDir(fixture.cartridgeRoot),
		name,
		"manifest.json",
	), []byte("corrupt\n"))
	fixture.plan = fillPlan(
		firstPlanID,
		firstVolume,
		"ABC123L6",
		[]string{name},
		[]backupplan.Action{action},
	)
	fixture.promote(t)

	err := fixture.run(t, func(context.Context, executor.BackupActionRequest) error {
		t.Fatal("backup handler was called for catalog member")
		return nil
	})
	if err != nil {
		t.Fatalf("RunSession error = %v", err)
	}
	session := fixture.readSession(t)
	assertEvent(t, session.Events, backupplan.EventItemSkipped, func(event backupplan.Event) bool {
		return event.MetadataRef == action.MetadataRef &&
			event.Reason == string(backupplan.ReasonAlreadyPresent)
	})
}

func TestRunSessionCatalogPrestateMismatchRetiresWithoutSweeping(t *testing.T) {
	fixture := newSessionFixture(t)
	expected := snapshotName(t, "tvdb:belief-only", 1)
	markerless := snapshotName(t, "tvdb:partial", 1)
	writeFile(t, filepath.Join(
		tapevolume.SnapshotsDir(fixture.cartridgeRoot),
		markerless,
		"tree",
		"partial.mkv",
	), []byte("partial"))
	fixture.plan = fillPlan(
		firstPlanID,
		firstVolume,
		"ABC123L6",
		[]string{expected},
		nil,
	)
	fixture.promote(t)

	if err := fixture.run(t, nil); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	if _, err := os.Lstat(filepath.Join(
		tapevolume.SnapshotsDir(fixture.cartridgeRoot),
		markerless,
	)); err != nil {
		t.Fatalf("pre-state refusal swept markerless entry: %v", err)
	}
	assertPlanDiscarded(t, fixture.stateRoot, firstPlanID)
}

func TestRunSessionHaltKeepsCommittedPrefixAndReleasesRemainder(t *testing.T) {
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
	normal := fixture.backupHandler(t)
	stopErr := errors.New("injected second-copy failure")
	handler := func(
		ctx context.Context,
		request executor.BackupActionRequest,
	) error {
		if request.Action.MetadataRef == second.MetadataRef {
			return stopErr
		}
		return normal(ctx, request)
	}

	if err := fixture.run(t, handler); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	assertCatalogSnapshot(
		t,
		fixture.stateRoot,
		firstVolume,
		snapshotName(t, first.MetadataRef, first.Generation),
		true,
	)
	assertCatalogSnapshot(
		t,
		fixture.stateRoot,
		firstVolume,
		snapshotName(t, second.MetadataRef, second.Generation),
		false,
	)
	assertPlanDiscarded(t, fixture.stateRoot, firstPlanID)
	replacement := fillPlan(
		secondPlanID,
		firstVolume,
		"ABC123L6",
		[]string{snapshotName(t, first.MetadataRef, first.Generation)},
		[]backupplan.Action{second},
	)
	if err := backupplan.Draft(fixture.stateRoot, replacement); err != nil {
		t.Fatalf("Draft replacement error = %v", err)
	}

	session := fixture.readSession(t)
	assertEvent(t, session.Events, backupplan.EventItemFailed, func(event backupplan.Event) bool {
		return event.MetadataRef == second.MetadataRef &&
			event.Generation == second.Generation &&
			event.Reason == string(backupplan.ReasonWriteFailed) &&
			event.Detail == stopErr.Error()
	})
	const haltDetail = "executor: plan failed: " +
		"executor: backup action (\"tvdb:b\", 1) failed: write_failed: " +
		"injected second-copy failure\ninjected second-copy failure"
	assertEvent(t, session.Events, backupplan.EventPlanFailed, func(event backupplan.Event) bool {
		return event.Reason == "halted" && event.Detail == haltDetail
	})
	library, err := planner.ReadLibrarySnapshot(fixture.libraryRoot, 0)
	if err != nil {
		t.Fatalf("ReadLibrarySnapshot error = %v", err)
	}
	catalog, err := planner.ReadCatalogSnapshot(fixture.stateRoot)
	if err != nil {
		t.Fatalf("ReadCatalogSnapshot error = %v", err)
	}
	report, err := planner.Consult(library, catalog, nil, 0)
	if err != nil {
		t.Fatalf("Consult error = %v", err)
	}
	if len(report.Pending) != 1 || report.Pending[0].MetadataRef != second.MetadataRef {
		t.Fatalf("pending after halt = %+v, want only %s", report.Pending, second.MetadataRef)
	}
}

func TestRunSessionUsesLiveCapacityInsteadOfStaleCatalogFreeBytes(t *testing.T) {
	fixture := newSessionFixtureWithCapacity(t, 32)
	action := writeSeries(t, fixture.libraryRoot, "Series A", "tvdb:a", 1)
	action.Bytes = 1 << 20
	fixture.plan = fillPlan(firstPlanID, firstVolume, "ABC123L6", nil, []backupplan.Action{action})
	fixture.promote(t)
	_, free, err := fixture.drive.Capacity()
	if err != nil {
		t.Fatalf("Capacity error = %v", err)
	}
	called := false

	if err := fixture.run(t, func(context.Context, executor.BackupActionRequest) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}
	if called {
		t.Fatal("backup handler was called despite insufficient live capacity")
	}
	assertPlanDiscarded(t, fixture.stateRoot, firstPlanID)
	session := fixture.readSession(t)
	wantDetail := fmt.Sprintf(
		"item needs %d bytes; 0-byte margin, %d bytes free",
		action.Bytes,
		free,
	)
	assertEvent(t, session.Events, backupplan.EventItemFailed, func(event backupplan.Event) bool {
		return event.MetadataRef == action.MetadataRef &&
			event.Generation == action.Generation &&
			event.Reason == string(backupplan.ReasonInsufficientSpace) &&
			event.Detail == wantDetail
	})
}

func TestRunSessionCancellationDuringBackupRetiresAndLogsExactReason(t *testing.T) {
	fixture := newSessionFixture(t)
	action := writeSeries(t, fixture.libraryRoot, "Series A", "tvdb:a", 1)
	fixture.plan = fillPlan(firstPlanID, firstVolume, "ABC123L6", nil, []backupplan.Action{action})
	fixture.promote(t)

	if err := fixture.run(t, func(context.Context, executor.BackupActionRequest) error {
		return context.Canceled
	}); err != nil {
		t.Fatalf("RunSession error = %v", err)
	}

	assertPlanDiscarded(t, fixture.stateRoot, firstPlanID)
	session := fixture.readSession(t)
	assertEvent(t, session.Events, backupplan.EventItemFailed, func(event backupplan.Event) bool {
		return event.MetadataRef == action.MetadataRef &&
			event.Generation == action.Generation &&
			event.Reason == string(backupplan.ReasonWriteFailed) &&
			event.Detail == context.Canceled.Error()
	})
	const haltDetail = "executor: plan failed: " +
		"executor: backup action (\"tvdb:a\", 1) failed: write_failed: " +
		"context canceled\ncontext canceled"
	assertEvent(t, session.Events, backupplan.EventPlanFailed, func(event backupplan.Event) bool {
		return event.Reason == "halted" && event.Detail == haltDetail
	})
}

type sessionFixture struct {
	stateRoot     string
	libraryRoot   string
	cartridgeRoot string
	drive         *executor.DirectoryDrive
	plan          backupplan.Plan
	journal       *backupplan.Writer
}

func newSessionFixture(t *testing.T) *sessionFixture {
	t.Helper()
	return newSessionFixtureWithCapacity(t, 1<<30)
}

func newSessionFixtureWithCapacity(t *testing.T, capacity int64) *sessionFixture {
	t.Helper()
	stateRoot := t.TempDir()
	libraryRoot := t.TempDir()
	cartridgeRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             cartridgeRoot,
		Mounted:          true,
		IdentityState:    executor.LoadedIdentityIdentified,
		MediumSerial:     "MAM-ABC123-0001",
		EncryptionActive: true,
		Capacity:         capacity,
	}}, "ABC123L6")
	installCatalogVolume(
		t,
		stateRoot,
		cartridgeRoot,
		firstVolume,
		"ABC123L6",
		capacity,
		capacity,
	)
	return &sessionFixture{
		stateRoot:     stateRoot,
		libraryRoot:   libraryRoot,
		cartridgeRoot: cartridgeRoot,
		drive:         drive,
	}
}

func (f *sessionFixture) promote(t *testing.T) {
	t.Helper()
	if err := backupplan.Draft(f.stateRoot, f.plan); err != nil {
		t.Fatalf("Draft error = %v", err)
	}
	if err := backupplan.Approve(f.stateRoot, f.plan.PlanID); err != nil {
		t.Fatalf("Approve error = %v", err)
	}
	f.journal = newSessionJournal(t, f.stateRoot, []string{f.plan.PlanID})
}

func (f *sessionFixture) backupHandler(t *testing.T) executor.BackupActionHandler {
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

func (f *sessionFixture) run(
	t *testing.T,
	handler executor.BackupActionHandler,
) error {
	t.Helper()
	if handler == nil {
		handler = f.backupHandler(t)
	}
	err := executor.RunSession(
		t.Context(),
		f.drive,
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

func (f *sessionFixture) readSession(t *testing.T) backupplan.Session {
	t.Helper()
	session, err := backupplan.ReadSession(f.stateRoot, sessionID)
	if err != nil {
		t.Fatalf("ReadSession error = %v", err)
	}
	return session
}

func fillPlan(
	planID string,
	volumeID volume.ID,
	tapeID tape.ID,
	existing []string,
	backups []backupplan.Action,
) backupplan.Plan {
	leading := append([]string{}, existing...)
	trailingSet := make(map[string]struct{}, len(existing)+len(backups))
	for _, name := range existing {
		trailingSet[name] = struct{}{}
	}
	for _, action := range backups {
		trailingSet[mustSnapshotName(action.MetadataRef, action.Generation)] = struct{}{}
	}
	trailing := make([]string, 0, len(trailingSet))
	for name := range trailingSet {
		trailing = append(trailing, name)
	}
	slices.Sort(leading)
	slices.Sort(trailing)
	actions := []backupplan.Action{
		{Type: backupplan.ActionAssertVolume, VolumeID: volumeID},
		{Type: backupplan.ActionAssertInventory, Snapshots: leading},
	}
	actions = append(actions, backups...)
	actions = append(actions, backupplan.Action{
		Type:      backupplan.ActionAssertInventory,
		Snapshots: trailing,
	})
	return backupplan.Plan{
		PlanID:    planID,
		CreatedAt: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		CreatedBy: backupplan.Creator{Version: "v0.6.0", Host: "tape-vm"},
		Target:    backupplan.Target{TapeID: tapeID},
		Actions:   actions,
	}
}

func backupAction(
	metadataRef, rootPath string,
	generation int,
	payloadFingerprint string,
	bytes int64,
) backupplan.Action {
	return backupplan.Action{
		Type:               backupplan.ActionBackup,
		MetadataRef:        metadataRef,
		RootPath:           rootPath,
		Generation:         generation,
		PayloadFingerprint: payloadFingerprint,
		Bytes:              bytes,
	}
}

func writeSeries(
	t *testing.T,
	libraryRoot, rootPath, metadataRef string,
	generation int,
) backupplan.Action {
	t.Helper()
	root := filepath.Join(libraryRoot, rootPath)
	writeFile(t, filepath.Join(root, "episode.mkv"), []byte("payload"))
	writeSeriesMetadata(t, root, seriesMetadata{
		Generation:  generation,
		MetadataRef: metadataRef,
	})
	digest, err := fingerprint.ComputeExcludingKura(root)
	if err != nil {
		t.Fatalf("ComputeExcludingKura error = %v", err)
	}
	return backupAction(metadataRef, rootPath, generation, string(digest), 1)
}

type seriesMetadata struct {
	Generation    int
	MetadataRef   string
	EpisodeStaged bool
	ActiveClaim   bool
}

func writeSeriesMetadata(t *testing.T, root string, metadata seriesMetadata) {
	t.Helper()
	episodes := map[string]any{}
	if metadata.EpisodeStaged {
		episodes["S01E01"] = map[string]any{"staged": map[string]any{}}
	}
	wire := map[string]any{
		"schemaVersion": 3,
		"generation":    metadata.Generation,
		"metadataRef":   metadata.MetadataRef,
		"episodes":      episodes,
		"stagedTrash":   []any{},
		"stagedExtras":  []any{},
	}
	if metadata.ActiveClaim {
		wire["in_progress"] = map[string]any{}
	}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("Marshal series metadata error = %v", err)
	}
	writeFile(t, filepath.Join(root, ".kura", "series.json"), append(data, '\n'))
}

func installCatalogVolume(
	t *testing.T,
	stateRoot, cartridgeRoot string,
	volumeID volume.ID,
	tapeID tape.ID,
	total, free int64,
) {
	t.Helper()
	header, err := os.ReadFile(tapevolume.VolumeFile(cartridgeRoot))
	if err != nil {
		t.Fatalf("ReadFile volume header error = %v", err)
	}
	if err := tapecatalog.InstallVolume(
		stateRoot,
		volumeID,
		header,
		tapecatalog.Observed{
			TapeID:        tapeID,
			ObservedAt:    time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
			CapacityBytes: total,
			FreeBytes:     free,
		},
	); err != nil {
		t.Fatalf("InstallVolume error = %v", err)
	}
}

func writeCommittedSnapshot(
	t *testing.T,
	cartridgeRoot string,
	action backupplan.Action,
	payload string,
) (name string, manifestBytes, completeBytes []byte) {
	t.Helper()
	name = snapshotName(t, action.MetadataRef, action.Generation)
	snapshotDir := filepath.Join(tapevolume.SnapshotsDir(cartridgeRoot), name)
	capturedAt := time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC)
	if err := tapemanifest.Write(snapshotDir, tapemanifest.Manifest{
		MetadataRef: action.MetadataRef,
		RootPath:    action.RootPath,
		Generation:  action.Generation,
		CapturedAt:  capturedAt,
		WrittenBy:   tapemanifest.Writer{Version: "v0.5.0", Host: "old-writer"},
		TotalBytes:  int64(len(payload)),
		Files: []tapemanifest.File{{
			Path:    "episode.mkv",
			Size:    int64(len(payload)),
			ModTime: capturedAt,
			Hash:    "sha256:" + strings.Repeat("0", 64),
		}},
	}); err != nil {
		t.Fatalf("Write manifest error = %v", err)
	}
	if err := tapemanifest.Commit(snapshotDir); err != nil {
		t.Fatalf("Commit manifest error = %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(snapshotDir, "manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile manifest error = %v", err)
	}
	completeBytes, err = os.ReadFile(filepath.Join(snapshotDir, "complete.json"))
	if err != nil {
		t.Fatalf("ReadFile complete error = %v", err)
	}
	return name, manifestBytes, completeBytes
}

func writeVolume(
	t *testing.T,
	root string,
	tapeID tape.ID,
	volumeID volume.ID,
) {
	t.Helper()
	if err := tapevolume.Write(root, tapevolume.Volume{
		VolumeID:       volumeID,
		TapeID:         tapeID,
		KeyFingerprint: "66687aadf862bd77",
		CreatedAt:      time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Write volume error = %v", err)
	}
	if err := os.MkdirAll(tapevolume.SnapshotsDir(root), 0o775); err != nil {
		t.Fatalf("MkdirAll snapshots error = %v", err)
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o775); err != nil {
		t.Fatalf("MkdirAll %q error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o664); err != nil {
		t.Fatalf("WriteFile %q error = %v", path, err)
	}
}

func newDrive(
	t *testing.T,
	cartridges []executor.DirectoryCartridge,
	loaded tape.ID,
) *executor.DirectoryDrive {
	t.Helper()
	for index := range cartridges {
		if cartridges[index].IdentityState == "" {
			cartridges[index].IdentityState = executor.LoadedIdentityIdentified
		}
		if cartridges[index].MediumSerial == "" {
			cartridges[index].MediumSerial = "MAM-" + string(cartridges[index].TapeID)
		}
	}
	drive, err := executor.NewDirectoryDrive(cartridges)
	if err != nil {
		t.Fatalf("NewDirectoryDrive error = %v", err)
	}
	if loaded != "" {
		if err := drive.SelectLoaded(loaded); err != nil {
			t.Fatalf("SelectLoaded error = %v", err)
		}
	}
	return drive
}

func newSessionJournal(
	t *testing.T,
	stateRoot string,
	readyPlans []string,
) *backupplan.Writer {
	t.Helper()
	journal, err := backupplan.CreateSession(stateRoot, backupplan.Header{
		SessionID:  sessionID,
		Executor:   backupplan.Creator{Version: "v0.6.0", Host: "tape-vm"},
		StartedAt:  time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		ReadyPlans: readyPlans,
	})
	if err != nil {
		t.Fatalf("CreateSession error = %v", err)
	}
	return journal
}

func assertPlanDiscarded(t *testing.T, stateRoot, planID string) {
	t.Helper()
	err := backupplan.Discard(stateRoot, planID)
	want := fmt.Sprintf("backupplan: discard %s: plan is already discarded", planID)
	assertExactError(t, err, want)
}

func assertCatalogSnapshot(
	t *testing.T,
	stateRoot string,
	volumeID volume.ID,
	name string,
	want bool,
) {
	t.Helper()
	dir, err := tapecatalog.VolumeDir(stateRoot, volumeID)
	if err != nil {
		t.Fatalf("VolumeDir error = %v", err)
	}
	_, err = tapemanifest.Read(filepath.Join(tapevolume.SnapshotsDir(dir), name))
	if want && err != nil {
		t.Fatalf("Read catalog snapshot %q error = %v", name, err)
	}
	if !want && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Read absent catalog snapshot %q error = %v, want file does not exist", name, err)
	}
}

func snapshotName(t *testing.T, metadataRef string, generation int) string {
	t.Helper()
	name, err := tapevolume.SnapshotName(metadataRef, generation)
	if err != nil {
		t.Fatalf("SnapshotName error = %v", err)
	}
	return name
}

func mustSnapshotName(metadataRef string, generation int) string {
	name, err := tapevolume.SnapshotName(metadataRef, generation)
	if err != nil {
		panic(err)
	}
	return name
}

func assertEvent(
	t *testing.T,
	events []backupplan.Event,
	eventType backupplan.EventType,
	match func(backupplan.Event) bool,
) {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType && match(event) {
			return
		}
	}
	t.Fatalf("event %q matching predicate not found in %#v", eventType, events)
}

func countEvents(events []backupplan.Event, eventType backupplan.EventType) int {
	count := 0
	for _, event := range events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

func assertExactError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("error = %q, want %q", errorString(err), want)
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

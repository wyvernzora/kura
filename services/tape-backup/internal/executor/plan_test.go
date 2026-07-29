package executor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/executor"
	"github.com/wyvernzora/kura/services/tape-backup/internal/fingerprint"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapemanifest"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

func TestRunSessionPassesExecutionDependenciesAndPostSweepCommittedSet(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	committed := makeSnapshot(t, cartridgeRoot, "tvdb:committed", 1, true)
	uncommitted := makeSnapshot(t, cartridgeRoot, "tvdb:uncommitted", 1, false)
	committedName := filepath.Base(committed)
	uncommittedName := filepath.Base(uncommitted)
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             cartridgeRoot,
		Mounted:          true,
		EncryptionActive: true,
		Capacity:         1 << 20,
	}}, "ABC123L6")
	outbox := newOutbox(t, &collectingSink{}, 16)
	options := fastSessionOptions
	options.LibraryRoot = libraryRoot
	called := false

	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{testPlan(firstPlanID, "ABC123L6", firstVolume)},
		outbox,
		func(_ context.Context, prepared executor.PreparedPlan) error {
			called = true
			if prepared.Drive != drive {
				t.Fatal("PreparedPlan.Drive does not carry the session drive")
			}
			if prepared.Events != outbox {
				t.Fatal("PreparedPlan.Events does not carry the session outbox")
			}
			if prepared.LibraryRoot != libraryRoot {
				t.Fatalf(
					"PreparedPlan.LibraryRoot = %q, want %q",
					prepared.LibraryRoot,
					libraryRoot,
				)
			}
			if _, ok := prepared.CommittedSnapshots[committedName]; !ok {
				t.Fatalf(
					"committed set = %#v, want %q",
					prepared.CommittedSnapshots,
					committedName,
				)
			}
			if _, ok := prepared.CommittedSnapshots[uncommittedName]; ok {
				t.Fatalf(
					"committed set contains swept %q: %#v",
					uncommittedName,
					prepared.CommittedSnapshots,
				)
			}
			return nil
		},
		options,
	)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	closeOutbox(t, outbox)
	if !called {
		t.Fatal("prepared-plan handler was not called")
	}
}

func TestExecutePreparedPlanClassifiesAlreadyPresentBeforePreflight(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	action := backupAction(
		"tvdb:already-present",
		"Series Moved Away",
		1,
		"sha256:"+strings.Repeat("0", 64),
		1<<30,
	)
	snapshotName, manifestBytes, completeBytes := writeCommittedSnapshot(
		t,
		cartridgeRoot,
		action,
	)
	markerPath := filepath.Join(
		tapevolume.SnapshotsDir(cartridgeRoot),
		snapshotName,
		"complete.json",
	)
	markerInfoBefore, err := os.Stat(markerPath)
	if err != nil {
		t.Fatalf("Stat completion marker before execution: %v", err)
	}
	drive := &fixedCapacityDrive{total: 100, free: 0}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		libraryRoot,
		map[string]struct{}{snapshotName: {}},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			action,
		},
	)
	copyCalls := 0

	execution, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		50,
		func(context.Context, executor.BackupActionRequest) error {
			copyCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, outbox)

	if copyCalls != 0 {
		t.Fatalf("backup seam calls = %d, want 0", copyCalls)
	}
	if drive.capacityCalls != 1 {
		t.Fatalf("Capacity calls = %d, want 1", drive.capacityCalls)
	}
	if len(execution.AlreadyPresent) != 1 {
		t.Fatalf(
			"already-present results = %d, want 1",
			len(execution.AlreadyPresent),
		)
	}
	got := execution.AlreadyPresent[0]
	if !bytes.Equal(got.Manifest, manifestBytes) {
		t.Fatalf("returned manifest bytes changed:\n%s", got.Manifest)
	}
	if !bytes.Equal(got.Complete, completeBytes) {
		t.Fatalf("returned completion bytes changed:\n%s", got.Complete)
	}
	markerInfoAfter, err := os.Stat(markerPath)
	if err != nil {
		t.Fatalf("Stat completion marker after execution: %v", err)
	}
	if !os.SameFile(markerInfoBefore, markerInfoAfter) ||
		!markerInfoBefore.ModTime().Equal(markerInfoAfter.ModTime()) {
		t.Fatal("already-present completion marker was replaced or touched")
	}
	assertEvent(t, sink.Events(), backupplan.EventItemSkipped, func(event backupplan.Event) bool {
		return event.MetadataRef == action.MetadataRef &&
			event.Generation == action.Generation &&
			event.Reason == string(backupplan.ReasonAlreadyPresent)
	})
	assertNoEventType(t, sink.Events(), backupplan.EventItemFailed)
}

func TestExecutePreparedPlanExcludesAlreadyPresentFromCumulativeBudget(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	existing := writeSeries(
		t,
		libraryRoot,
		"Existing",
		"tvdb:existing",
		1,
		nil,
	)
	existing.Bytes = 100
	existingName, _, _ := writeCommittedSnapshot(t, cartridgeRoot, existing)
	newAction := writeSeries(t, libraryRoot, "New", "tvdb:new", 1, nil)
	newAction.Bytes = 8
	drive := &fixedCapacityDrive{total: 10, free: 10}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		libraryRoot,
		map[string]struct{}{existingName: {}},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			existing,
			newAction,
		},
	)
	var copied []string

	_, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		2,
		func(_ context.Context, request executor.BackupActionRequest) error {
			copied = append(copied, request.Action.MetadataRef)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, outbox)

	if want := []string{newAction.MetadataRef}; !reflect.DeepEqual(copied, want) {
		t.Fatalf("copied items = %v, want %v", copied, want)
	}
	assertNoDroppedReason(
		t,
		sink.Events(),
		newAction.MetadataRef,
		backupplan.ReasonInsufficientSpace,
	)
}

func TestExecutePreparedPlanReleasesNoHealingBytesOnCapacityFailure(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	existing := writeSeries(
		t,
		libraryRoot,
		"Existing",
		"tvdb:existing",
		1,
		nil,
	)
	existingName, _, _ := writeCommittedSnapshot(
		t,
		cartridgeRoot,
		existing,
	)
	pending := writeSeries(t, libraryRoot, "Pending", "tvdb:pending", 1, nil)
	capacityErr := errors.New("capacity unavailable")
	drive := &fixedCapacityDrive{capacityErr: capacityErr}
	prepared, _, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		libraryRoot,
		map[string]struct{}{existingName: {}},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			existing,
			pending,
		},
	)

	execution, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(context.Context, executor.BackupActionRequest) error {
			t.Fatal("backup seam was called after capacity failure")
			return nil
		},
	)
	if !errors.Is(err, executor.ErrPlanFailed) {
		t.Fatalf("ExecutePreparedPlan error = %v, want ErrPlanFailed", err)
	}
	if !errors.Is(err, capacityErr) {
		t.Fatalf("ExecutePreparedPlan error = %v, want capacity error", err)
	}
	closeOutbox(t, outbox)

	if !reflect.DeepEqual(execution, executor.PreparedExecution{}) {
		t.Fatalf("execution = %#v, want zero result", execution)
	}
}

func TestExecutePreparedPlanUsesPassedPostSweepCommittedSet(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	action := writeSeries(t, libraryRoot, "Series", "tvdb:series", 1, nil)
	writeCommittedSnapshot(t, cartridgeRoot, action)
	drive := &fixedCapacityDrive{total: 100, free: 100}
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
	copyCalls := 0

	execution, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(context.Context, executor.BackupActionRequest) error {
			copyCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, outbox)

	if copyCalls != 1 {
		t.Fatalf("backup seam calls = %d, want 1", copyCalls)
	}
	if len(execution.AlreadyPresent) != 0 {
		t.Fatalf(
			"already-present results = %#v, want none from the passed empty set",
			execution.AlreadyPresent,
		)
	}
	assertNoEventType(t, sink.Events(), backupplan.EventItemSkipped)
}

func TestExecutePreparedPlanRejectsMarkedUnreadableTargetWithoutWriting(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	action := writeSeries(
		t,
		libraryRoot,
		"Corrupt",
		"tvdb:corrupt",
		1,
		nil,
	)
	name, _, _ := writeCommittedSnapshot(t, cartridgeRoot, action)
	markerPath := filepath.Join(
		tapevolume.SnapshotsDir(cartridgeRoot),
		name,
		"complete.json",
	)
	corruptMarker := []byte("{\"schemaVersion\":1,\"manifestHash\":\"sha256:" +
		strings.Repeat("0", 64) +
		"\",\"completedAt\":\"2026-07-27T09:00:00Z\"}\n")
	writeFile(t, markerPath, corruptMarker)
	drive := &fixedCapacityDrive{total: 100, free: 100}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		libraryRoot,
		map[string]struct{}{name: {}},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			action,
		},
	)
	copyCalls := 0

	_, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(context.Context, executor.BackupActionRequest) error {
			copyCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, outbox)

	if copyCalls != 0 {
		t.Fatalf("backup seam calls = %d, want 0", copyCalls)
	}
	if drive.capacityCalls != 1 {
		t.Fatalf("Capacity calls = %d, want 1", drive.capacityCalls)
	}
	gotMarker, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("Read corrupt completion marker: %v", err)
	}
	if !bytes.Equal(gotMarker, corruptMarker) {
		t.Fatalf("corrupt completion marker was modified:\n%s", gotMarker)
	}
	assertEvent(t, sink.Events(), backupplan.EventItemFailed, func(event backupplan.Event) bool {
		return event.MetadataRef == action.MetadataRef &&
			event.Reason == string(backupplan.ReasonChecksumMismatch)
	})
}

func TestExecutePreparedPlanRefusesOutOfScopeActionBeforeExecution(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	action := writeSeries(t, libraryRoot, "Series", "tvdb:series", 1, nil)
	drive := &fixedCapacityDrive{total: 100, free: 100}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		libraryRoot,
		map[string]struct{}{},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			action,
			{Type: backupplan.ActionReformat},
		},
	)
	copyCalls := 0

	_, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(context.Context, executor.BackupActionRequest) error {
			copyCalls++
			return nil
		},
	)
	if copyCalls != 0 {
		t.Fatalf("backup seam calls = %d, want 0", copyCalls)
	}
	assertExactError(
		t,
		err,
		`executor: plan `+firstPlanID+` contains out-of-scope action "reformat"`,
	)
	closeOutbox(t, outbox)

	if len(sink.Events()) != 0 {
		t.Fatalf("events = %#v, want none", sink.Events())
	}
}

func TestExecutePreparedPlanPreflightDropsOnlyInvalidItem(t *testing.T) {
	tests := []struct {
		name   string
		reason backupplan.ItemReason
		mutate func(*testing.T, string, *backupplan.Action)
	}{
		{
			name:   "generation newer than pin",
			reason: backupplan.ReasonSeriesMoved,
			mutate: func(t *testing.T, root string, _ *backupplan.Action) {
				writeSeriesMetadata(t, root, seriesMetadata{
					Generation:  2,
					MetadataRef: "tvdb:invalid",
				})
			},
		},
		{
			name:   "generation older than pin",
			reason: backupplan.ReasonSeriesMoved,
			mutate: func(t *testing.T, _ string, action *backupplan.Action) {
				action.Generation = 2
			},
		},
		{
			name:   "payload fingerprint changed",
			reason: backupplan.ReasonPayloadDrift,
			mutate: func(t *testing.T, root string, _ *backupplan.Action) {
				payload := filepath.Join(root, "episode.mkv")
				info, err := os.Stat(payload)
				if err != nil {
					t.Fatalf("Stat payload: %v", err)
				}
				changed := info.ModTime().Add(time.Second)
				if err := os.Chtimes(payload, changed, changed); err != nil {
					t.Fatalf("Chtimes payload: %v", err)
				}
			},
		},
		{
			name:   "episode staged",
			reason: backupplan.ReasonStagedIntent,
			mutate: func(t *testing.T, root string, _ *backupplan.Action) {
				writeSeriesMetadata(t, root, seriesMetadata{
					Generation:    1,
					MetadataRef:   "tvdb:invalid",
					EpisodeStaged: true,
				})
			},
		},
		{
			name:   "trash staged",
			reason: backupplan.ReasonStagedIntent,
			mutate: func(t *testing.T, root string, _ *backupplan.Action) {
				writeSeriesMetadata(t, root, seriesMetadata{
					Generation:  1,
					MetadataRef: "tvdb:invalid",
					StagedTrash: true,
				})
			},
		},
		{
			name:   "extra staged",
			reason: backupplan.ReasonStagedIntent,
			mutate: func(t *testing.T, root string, _ *backupplan.Action) {
				writeSeriesMetadata(t, root, seriesMetadata{
					Generation:   1,
					MetadataRef:  "tvdb:invalid",
					StagedExtras: true,
				})
			},
		},
		{
			name:   "active claim",
			reason: backupplan.ReasonClaimStale,
			mutate: func(t *testing.T, root string, _ *backupplan.Action) {
				writeSeriesMetadata(t, root, seriesMetadata{
					Generation:  1,
					MetadataRef: "tvdb:invalid",
					ActiveClaim: true,
				})
			},
		},
		{
			name:   "different metadata ref",
			reason: backupplan.ReasonSeriesMoved,
			mutate: func(t *testing.T, root string, _ *backupplan.Action) {
				writeSeriesMetadata(t, root, seriesMetadata{
					Generation:  1,
					MetadataRef: "tvdb:different",
				})
			},
		},
		{
			name:   "series root missing",
			reason: backupplan.ReasonSeriesRootMissing,
			mutate: func(t *testing.T, root string, _ *backupplan.Action) {
				if err := os.RemoveAll(root); err != nil {
					t.Fatalf("RemoveAll series root: %v", err)
				}
			},
		},
		{
			name:   "unsupported symlink",
			reason: backupplan.ReasonUnsupportedFileType,
			mutate: func(t *testing.T, root string, _ *backupplan.Action) {
				if err := os.Symlink("episode.mkv", filepath.Join(root, "link.mkv")); err != nil {
					t.Fatalf("Symlink payload: %v", err)
				}
			},
		},
		{
			name:   "walk error under path containing symlink",
			reason: backupplan.ReasonPayloadDrift,
			mutate: func(t *testing.T, root string, _ *backupplan.Action) {
				unreadable := filepath.Join(root, "symlink")
				if err := os.Mkdir(unreadable, 0o755); err != nil {
					t.Fatalf("Mkdir unreadable directory: %v", err)
				}
				if err := os.Chmod(unreadable, 0); err != nil {
					t.Fatalf("Chmod unreadable directory: %v", err)
				}
				t.Cleanup(func() {
					if err := os.Chmod(unreadable, 0o755); err != nil {
						t.Errorf("restore unreadable directory mode: %v", err)
					}
				})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cartridgeRoot := t.TempDir()
			libraryRoot := t.TempDir()
			writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
			invalid := writeSeries(
				t,
				libraryRoot,
				"Invalid",
				"tvdb:invalid",
				1,
				nil,
			)
			valid := writeSeries(
				t,
				libraryRoot,
				"Valid",
				"tvdb:valid",
				1,
				nil,
			)
			tc.mutate(
				t,
				filepath.Join(libraryRoot, "Invalid"),
				&invalid,
			)
			drive := &fixedCapacityDrive{total: 100, free: 100}
			prepared, sink, outbox := preparedPlan(
				t,
				drive,
				cartridgeRoot,
				libraryRoot,
				map[string]struct{}{},
				[]backupplan.Action{
					{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
					invalid,
					valid,
				},
			)
			var copied []string

			_, err := executor.ExecutePreparedPlan(
				t.Context(),
				prepared,
				0,
				func(_ context.Context, request executor.BackupActionRequest) error {
					copied = append(copied, request.Action.MetadataRef)
					return nil
				},
			)
			if err != nil {
				t.Fatalf("ExecutePreparedPlan: %v", err)
			}
			closeOutbox(t, outbox)

			if want := []string{valid.MetadataRef}; !reflect.DeepEqual(copied, want) {
				t.Fatalf("copied items = %v, want %v", copied, want)
			}
			assertDroppedReason(
				t,
				sink.Events(),
				invalid.MetadataRef,
				invalid.Generation,
				tc.reason,
			)
			assertEvent(
				t,
				sink.Events(),
				backupplan.EventItemFailed,
				func(event backupplan.Event) bool {
					return event.MetadataRef == invalid.MetadataRef &&
						event.Generation == invalid.Generation &&
						event.Reason == string(tc.reason)
				},
			)
		})
	}
}

func TestExecutePreparedPlanCumulativeFitKeepsDeterministicPrefix(t *testing.T) {
	tests := []struct {
		name       string
		free       int64
		wantCopied []string
		wantDrop   string
	}{
		{
			name:       "exact cumulative fit",
			free:       14,
			wantCopied: []string{"tvdb:first", "tvdb:second", "tvdb:third"},
		},
		{
			name:       "one byte short drops trailing item",
			free:       13,
			wantCopied: []string{"tvdb:first", "tvdb:second"},
			wantDrop:   "tvdb:third",
		},
		{
			name:       "individually fit but collectively overflow",
			free:       10,
			wantCopied: []string{"tvdb:first", "tvdb:second"},
			wantDrop:   "tvdb:third",
		},
		{
			name:       "overflow drops every trailing item",
			free:       6,
			wantCopied: []string{"tvdb:first"},
			wantDrop:   "tvdb:second",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cartridgeRoot := t.TempDir()
			libraryRoot := t.TempDir()
			writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
			actions := []backupplan.Action{
				{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			}
			for _, item := range []struct {
				root string
				ref  string
			}{
				{root: "First", ref: "tvdb:first"},
				{root: "Second", ref: "tvdb:second"},
				{root: "Third", ref: "tvdb:third"},
			} {
				action := writeSeries(
					t,
					libraryRoot,
					item.root,
					item.ref,
					1,
					nil,
				)
				action.Bytes = 4
				actions = append(actions, action)
			}
			drive := &fixedCapacityDrive{total: tc.free, free: tc.free}
			prepared, sink, outbox := preparedPlan(
				t,
				drive,
				cartridgeRoot,
				libraryRoot,
				map[string]struct{}{},
				actions,
			)
			var copied []string

			_, err := executor.ExecutePreparedPlan(
				t.Context(),
				prepared,
				2,
				func(_ context.Context, request executor.BackupActionRequest) error {
					copied = append(copied, request.Action.MetadataRef)
					return nil
				},
			)
			if err != nil {
				t.Fatalf("ExecutePreparedPlan: %v", err)
			}
			closeOutbox(t, outbox)

			if !reflect.DeepEqual(copied, tc.wantCopied) {
				t.Fatalf("copied items = %v, want %v", copied, tc.wantCopied)
			}
			if tc.wantDrop != "" {
				assertDroppedReason(
					t,
					sink.Events(),
					tc.wantDrop,
					1,
					backupplan.ReasonInsufficientSpace,
				)
				if tc.free == 6 {
					assertDroppedReason(
						t,
						sink.Events(),
						"tvdb:third",
						1,
						backupplan.ReasonInsufficientSpace,
					)
				}
			} else {
				assertNoDroppedReason(
					t,
					sink.Events(),
					"tvdb:third",
					backupplan.ReasonInsufficientSpace,
				)
			}
		})
	}
}

func TestExecutePreparedPlanTrailingInventoryExcludesPreflightDrop(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	dropped := writeSeries(
		t,
		libraryRoot,
		"Dropped",
		"tvdb:dropped",
		1,
		&seriesMetadata{StagedTrash: true},
	)
	droppedName := snapshotName(t, dropped.MetadataRef, dropped.Generation)
	mkdir(t, filepath.Join(tapevolume.SnapshotsDir(cartridgeRoot), droppedName))
	drive := &fixedCapacityDrive{total: 100, free: 100}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		libraryRoot,
		map[string]struct{}{},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			dropped,
			{
				Type:      backupplan.ActionAssertInventory,
				Snapshots: []string{droppedName},
			},
		},
	)

	_, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(context.Context, executor.BackupActionRequest) error {
			t.Fatal("backup seam was called for dropped item")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, outbox)

	assertEvent(
		t,
		sink.Events(),
		backupplan.EventDivergenceChecked,
		func(event backupplan.Event) bool {
			return event.Result == "match" && len(event.ExtraSnapshots) == 0
		},
	)
	assertNoEventType(t, sink.Events(), backupplan.EventPlanFailed)
}

func TestExecutePreparedPlanTrailingInventoryRequiresAcceptedBackupMarker(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	action := writeSeries(t, libraryRoot, "Accepted", "tvdb:accepted", 1, nil)
	name := snapshotName(t, action.MetadataRef, action.Generation)
	drive := &fixedCapacityDrive{total: 100, free: 100}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		libraryRoot,
		map[string]struct{}{},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			action,
			{
				Type:      backupplan.ActionAssertInventory,
				Snapshots: []string{name},
			},
		},
	)
	copyCalls := 0

	_, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(context.Context, executor.BackupActionRequest) error {
			copyCalls++
			return nil
		},
	)
	if !errors.Is(err, executor.ErrPlanFailed) {
		t.Fatalf("ExecutePreparedPlan error = %v, want ErrPlanFailed", err)
	}
	closeOutbox(t, outbox)

	if copyCalls != 1 {
		t.Fatalf("backup seam calls = %d, want 1", copyCalls)
	}
	assertEvent(
		t,
		sink.Events(),
		backupplan.EventDivergenceChecked,
		func(event backupplan.Event) bool {
			return event.Result == "missing"
		},
	)
}

func TestExecutePreparedPlanTrailingInventoryKeepsAlreadyPresentExpected(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	action := writeSeries(t, libraryRoot, "Existing", "tvdb:existing", 1, nil)
	name, _, _ := writeCommittedSnapshot(t, cartridgeRoot, action)
	drive := &fixedCapacityDrive{total: 100, free: 100}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		libraryRoot,
		map[string]struct{}{name: {}},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			action,
			{
				Type:      backupplan.ActionAssertInventory,
				Snapshots: []string{name},
			},
		},
	)

	_, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(context.Context, executor.BackupActionRequest) error {
			t.Fatal("backup seam was called for already-present item")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, outbox)

	assertEvent(
		t,
		sink.Events(),
		backupplan.EventDivergenceChecked,
		func(event backupplan.Event) bool {
			return event.Result == "match" && len(event.ExtraSnapshots) == 0
		},
	)
}

func TestExecutePreparedPlanAssertVolumeRereadsMountedHeader(t *testing.T) {
	cartridgeRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", secondVolume)
	drive := &fixedCapacityDrive{total: 100, free: 100}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		t.TempDir(),
		map[string]struct{}{},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
		},
	)
	prepared.Volume.VolumeID = firstVolume

	_, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(context.Context, executor.BackupActionRequest) error { return nil },
	)
	if !errors.Is(err, executor.ErrPlanFailed) {
		t.Fatalf("ExecutePreparedPlan error = %v, want ErrPlanFailed", err)
	}
	closeOutbox(t, outbox)
	assertEvent(t, sink.Events(), backupplan.EventPlanFailed, func(event backupplan.Event) bool {
		return event.Reason == string(backupplan.ActionAssertVolume) &&
			strings.Contains(event.Detail, string(secondVolume))
	})
}

func TestRunSessionTreatsFailedAssertionAsFailedPlan(t *testing.T) {
	cartridgeRoot := t.TempDir()
	stateRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             cartridgeRoot,
		Mounted:          true,
		EncryptionActive: true,
		Capacity:         1 << 20,
	}}, "ABC123L6")
	sink := &collectingSink{}
	journal := newSessionJournal(t, stateRoot)
	defer closeSessionJournal(t, journal)
	outbox := newOutboxWithJournal(t, journal, sink, 32)
	options := fastSessionOptions
	options.LibraryRoot = libraryRoot

	err := executor.RunSession(
		t.Context(),
		drive,
		[]backupplan.Plan{testPlan(firstPlanID, "ABC123L6", firstVolume)},
		outbox,
		func(ctx context.Context, prepared executor.PreparedPlan) error {
			writeVolume(t, cartridgeRoot, "ABC123L6", secondVolume)
			_, err := executor.ExecutePreparedPlan(
				ctx,
				prepared,
				0,
				func(context.Context, executor.BackupActionRequest) error {
					t.Fatal("backup seam was called after assert_volume failed")
					return nil
				},
			)
			return err
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
	if got, want := session.Terminal.Reason, "plans_failed"; got != want {
		t.Fatalf("terminal reason = %q, want %q", got, want)
	}
	if got := session.Terminal.PlansCompleted; got != 0 {
		t.Fatalf("terminal plansCompleted = %d, want 0", got)
	}
	assertEvent(t, sink.Events(), backupplan.EventPlanFailed, func(event backupplan.Event) bool {
		return event.Reason == string(backupplan.ActionAssertVolume)
	})
}

func TestExecutePreparedPlanAssertInventoryRequiresCompletionMarker(t *testing.T) {
	cartridgeRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	expected := snapshotName(t, "tvdb:marker-required", 1)
	mkdir(t, filepath.Join(tapevolume.SnapshotsDir(cartridgeRoot), expected))
	drive := &fixedCapacityDrive{total: 100, free: 100}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		t.TempDir(),
		map[string]struct{}{},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			{Type: backupplan.ActionAssertInventory, Snapshots: []string{expected}},
		},
	)

	_, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(context.Context, executor.BackupActionRequest) error { return nil },
	)
	if !errors.Is(err, executor.ErrPlanFailed) {
		t.Fatalf("ExecutePreparedPlan error = %v, want ErrPlanFailed", err)
	}
	closeOutbox(t, outbox)
	assertEvent(
		t,
		sink.Events(),
		backupplan.EventDivergenceChecked,
		func(event backupplan.Event) bool {
			return event.Result == "missing"
		},
	)
}

func TestExecutePreparedPlanAssertInventoryReportsExtraCommittedSnapshots(t *testing.T) {
	cartridgeRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	expected := snapshotName(t, "tvdb:expected", 1)
	extraA := snapshotName(t, "tvdb:extra-a", 1)
	extraB := snapshotName(t, "tvdb:extra-b", 2)
	for _, name := range []string{expected, extraB, extraA} {
		writeFile(
			t,
			filepath.Join(
				tapevolume.SnapshotsDir(cartridgeRoot),
				name,
				"complete.json",
			),
			[]byte("{}\n"),
		)
	}
	drive := &fixedCapacityDrive{total: 100, free: 100}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		t.TempDir(),
		map[string]struct{}{},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			{Type: backupplan.ActionAssertInventory, Snapshots: []string{expected}},
		},
	)

	_, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(context.Context, executor.BackupActionRequest) error { return nil },
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, outbox)
	assertEvent(
		t,
		sink.Events(),
		backupplan.EventDivergenceChecked,
		func(event backupplan.Event) bool {
			return event.Result == "match" &&
				reflect.DeepEqual(event.ExtraSnapshots, []string{extraA, extraB})
		},
	)
}

func TestExecutePreparedPlanAssertInventoryExcludesChecksumMismatchFromExtras(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	action := writeSeries(t, libraryRoot, "Corrupt", "tvdb:corrupt", 1, nil)
	name, _, _ := writeCommittedSnapshot(t, cartridgeRoot, action)
	writeFile(
		t,
		filepath.Join(
			tapevolume.SnapshotsDir(cartridgeRoot),
			name,
			"complete.json",
		),
		[]byte("{\"schemaVersion\":1,\"manifestHash\":\"sha256:"+
			strings.Repeat("0", 64)+
			"\",\"completedAt\":\"2026-07-27T09:00:00Z\"}\n"),
	)
	drive := &fixedCapacityDrive{total: 100, free: 100}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		libraryRoot,
		map[string]struct{}{name: {}},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			action,
			{
				Type:      backupplan.ActionAssertInventory,
				Snapshots: []string{name},
			},
		},
	)

	_, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(context.Context, executor.BackupActionRequest) error {
			t.Fatal("backup seam was called for checksum-mismatched item")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, outbox)

	assertEvent(t, sink.Events(), backupplan.EventItemFailed, func(event backupplan.Event) bool {
		return event.MetadataRef == action.MetadataRef &&
			event.Reason == string(backupplan.ReasonChecksumMismatch)
	})
	assertEvent(
		t,
		sink.Events(),
		backupplan.EventDivergenceChecked,
		func(event backupplan.Event) bool {
			return event.Result == "match" && len(event.ExtraSnapshots) == 0
		},
	)
}

func TestExecutePreparedPlanAssertionsRunInActionOrder(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	action := writeSeries(t, libraryRoot, "New", "tvdb:new", 1, nil)
	name := snapshotName(t, action.MetadataRef, action.Generation)
	drive := &fixedCapacityDrive{total: 100, free: 100}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		libraryRoot,
		map[string]struct{}{},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			action,
			{Type: backupplan.ActionAssertInventory, Snapshots: []string{name}},
		},
	)

	_, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(_ context.Context, request executor.BackupActionRequest) error {
			writeFile(
				t,
				filepath.Join(
					tapevolume.SnapshotsDir(request.Cartridge.Root),
					name,
					"complete.json",
				),
				[]byte("{}\n"),
			)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("ExecutePreparedPlan: %v", err)
	}
	closeOutbox(t, outbox)
	assertEvent(
		t,
		sink.Events(),
		backupplan.EventDivergenceChecked,
		func(event backupplan.Event) bool {
			return event.Result == "match"
		},
	)
}

func TestExecutePreparedPlanFailedAssertionAbortsAtItsPosition(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	first := writeSeries(t, libraryRoot, "First", "tvdb:first", 1, nil)
	second := writeSeries(t, libraryRoot, "Second", "tvdb:second", 1, nil)
	drive := &fixedCapacityDrive{total: 100, free: 9}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		libraryRoot,
		map[string]struct{}{},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			first,
			{Type: backupplan.ActionAssertFreeSpace, Bytes: 10},
			second,
		},
	)
	var copied []string

	_, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(_ context.Context, request executor.BackupActionRequest) error {
			copied = append(copied, request.Action.MetadataRef)
			return nil
		},
	)
	if !errors.Is(err, executor.ErrPlanFailed) {
		t.Fatalf("ExecutePreparedPlan error = %v, want ErrPlanFailed", err)
	}
	closeOutbox(t, outbox)

	if want := []string{first.MetadataRef}; !reflect.DeepEqual(copied, want) {
		t.Fatalf("copied items = %v, want %v", copied, want)
	}
	assertEvent(t, sink.Events(), backupplan.EventPlanFailed, func(event backupplan.Event) bool {
		return event.Reason == string(backupplan.ActionAssertFreeSpace)
	})
}

func TestExecutePreparedPlanAssertFreeSpaceUsesAtLeastComparison(t *testing.T) {
	tests := []struct {
		name     string
		free     int64
		wantFail bool
	}{
		{name: "exactly pinned bytes", free: 10},
		{name: "one byte below pin", free: 9, wantFail: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cartridgeRoot := t.TempDir()
			writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
			drive := &fixedCapacityDrive{total: 10, free: tc.free}
			prepared, sink, outbox := preparedPlan(
				t,
				drive,
				cartridgeRoot,
				t.TempDir(),
				map[string]struct{}{},
				[]backupplan.Action{
					{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
					{Type: backupplan.ActionAssertFreeSpace, Bytes: 10},
				},
			)

			_, err := executor.ExecutePreparedPlan(
				t.Context(),
				prepared,
				0,
				func(context.Context, executor.BackupActionRequest) error { return nil },
			)
			closeOutbox(t, outbox)
			if tc.wantFail {
				if !errors.Is(err, executor.ErrPlanFailed) {
					t.Fatalf("error = %v, want ErrPlanFailed", err)
				}
				assertEvent(
					t,
					sink.Events(),
					backupplan.EventPlanFailed,
					func(event backupplan.Event) bool {
						return event.Reason ==
							string(backupplan.ActionAssertFreeSpace)
					},
				)
				return
			}
			if err != nil {
				t.Fatalf("ExecutePreparedPlan: %v", err)
			}
			assertNoEventType(t, sink.Events(), backupplan.EventPlanFailed)
		})
	}
}

func TestExecutePreparedPlanAssertFreeSpaceCapacityFailureAbortsPlan(t *testing.T) {
	cartridgeRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	capacityErr := errors.New("capacity unavailable")
	drive := &fixedCapacityDrive{capacityErr: capacityErr}
	prepared, sink, outbox := preparedPlan(
		t,
		drive,
		cartridgeRoot,
		t.TempDir(),
		map[string]struct{}{},
		[]backupplan.Action{
			{Type: backupplan.ActionAssertVolume, VolumeID: firstVolume},
			{Type: backupplan.ActionAssertFreeSpace, Bytes: 1},
		},
	)

	_, err := executor.ExecutePreparedPlan(
		t.Context(),
		prepared,
		0,
		func(context.Context, executor.BackupActionRequest) error { return nil },
	)
	if !errors.Is(err, executor.ErrPlanFailed) {
		t.Fatalf("ExecutePreparedPlan error = %v, want ErrPlanFailed", err)
	}
	closeOutbox(t, outbox)
	assertEvent(t, sink.Events(), backupplan.EventPlanFailed, func(event backupplan.Event) bool {
		return event.Reason == string(backupplan.ActionAssertFreeSpace) &&
			strings.Contains(event.Detail, capacityErr.Error())
	})
}

func TestExecutePreparedPlanCancellationStopsBeforeNextAction(t *testing.T) {
	cartridgeRoot := t.TempDir()
	libraryRoot := t.TempDir()
	writeVolume(t, cartridgeRoot, "ABC123L6", firstVolume)
	action := writeSeries(t, libraryRoot, "Series", "tvdb:series", 1, nil)
	drive := &fixedCapacityDrive{total: 100, free: 100}
	prepared, _, outbox := preparedPlan(
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
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := executor.ExecutePreparedPlan(
		ctx,
		prepared,
		0,
		func(context.Context, executor.BackupActionRequest) error {
			t.Fatal("backup seam was called after cancellation")
			return nil
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecutePreparedPlan error = %v, want context.Canceled", err)
	}
	closeOutbox(t, outbox)
}

type fixedCapacityDrive struct {
	total         int64
	free          int64
	capacityErr   error
	capacityCalls int
	syncErr       error
	syncCalls     int
}

func (*fixedCapacityDrive) Open() error {
	return errors.New("Open must not be called")
}

func (*fixedCapacityDrive) Close() error {
	return errors.New("Close must not be called")
}

func (*fixedCapacityDrive) LoadedIdentity() (executor.LoadedIdentity, error) {
	return executor.LoadedIdentity{}, errors.New("LoadedIdentity must not be called")
}

func (d *fixedCapacityDrive) EncryptionActive() (bool, error) {
	return false, errors.New("EncryptionActive must not be called")
}

func (d *fixedCapacityDrive) Capacity() (total, free int64, err error) {
	d.capacityCalls++
	if d.capacityErr != nil {
		return 0, 0, d.capacityErr
	}
	return d.total, d.free, nil
}

func (d *fixedCapacityDrive) Sync() error {
	d.syncCalls++
	return d.syncErr
}

func (*fixedCapacityDrive) Format(tape.ID) error {
	return errors.New("Format must not be called")
}

func (*fixedCapacityDrive) StampIdentity(volume.ID, tape.ID) error {
	return errors.New("StampIdentity must not be called")
}

type seriesMetadata struct {
	Generation    int
	MetadataRef   string
	EpisodeStaged bool
	StagedTrash   bool
	StagedExtras  bool
	ActiveClaim   bool
}

func preparedPlan(
	t *testing.T,
	drive executor.Drive,
	cartridgeRoot, libraryRoot string,
	committed map[string]struct{},
	actions []backupplan.Action,
) (executor.PreparedPlan, *collectingSink, *executor.EventOutbox) {
	t.Helper()
	sink := &collectingSink{}
	outbox := newOutbox(t, sink, 64)
	return executor.PreparedPlan{
		Drive:  drive,
		Events: outbox,
		Cartridge: executor.Cartridge{
			TapeID: "ABC123L6",
			Root:   cartridgeRoot,
		},
		Volume: tapevolume.Volume{
			VolumeID: firstVolume,
			TapeID:   "ABC123L6",
		},
		Plan: backupplan.Plan{
			PlanID:  firstPlanID,
			Target:  backupplan.Target{TapeID: "ABC123L6"},
			Actions: actions,
		},
		LibraryRoot:        libraryRoot,
		CommittedSnapshots: committed,
	}, sink, outbox
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
	metadataOverrides *seriesMetadata,
) backupplan.Action {
	t.Helper()
	root := filepath.Join(libraryRoot, rootPath)
	writeFile(t, filepath.Join(root, "episode.mkv"), []byte("payload"))
	metadata := seriesMetadata{
		Generation:  generation,
		MetadataRef: metadataRef,
	}
	if metadataOverrides != nil {
		metadata.EpisodeStaged = metadataOverrides.EpisodeStaged
		metadata.StagedTrash = metadataOverrides.StagedTrash
		metadata.StagedExtras = metadataOverrides.StagedExtras
		metadata.ActiveClaim = metadataOverrides.ActiveClaim
	}
	writeSeriesMetadata(t, root, metadata)
	digest, err := fingerprint.ComputeExcludingKura(root)
	if err != nil {
		t.Fatalf("ComputeExcludingKura: %v", err)
	}
	return backupAction(metadataRef, rootPath, generation, string(digest), 1)
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
	if metadata.StagedTrash {
		wire["stagedTrash"] = []any{map[string]any{}}
	}
	if metadata.StagedExtras {
		wire["stagedExtras"] = []any{map[string]any{}}
	}
	if metadata.ActiveClaim {
		wire["in_progress"] = map[string]any{}
	}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("Marshal series metadata: %v", err)
	}
	writeFile(t, filepath.Join(root, ".kura", "series.json"), append(data, '\n'))
}

func writeCommittedSnapshot(
	t *testing.T,
	cartridgeRoot string,
	action backupplan.Action,
) (name string, manifestBytes, completeBytes []byte) {
	t.Helper()
	name = snapshotName(t, action.MetadataRef, action.Generation)
	snapshotDir := filepath.Join(tapevolume.SnapshotsDir(cartridgeRoot), name)
	capturedAt := time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC)
	err := tapemanifest.Write(snapshotDir, tapemanifest.Manifest{
		MetadataRef: action.MetadataRef,
		RootPath:    action.RootPath,
		Generation:  action.Generation,
		CapturedAt:  capturedAt,
		WrittenBy: tapemanifest.Writer{
			Version: "v0.6.0",
			Host:    "tape-vm",
		},
		TotalBytes: 1,
		Files: []tapemanifest.File{{
			Path:    "episode.mkv",
			Size:    1,
			ModTime: capturedAt,
			Hash:    "sha256:" + strings.Repeat("0", 64),
		}},
	})
	if err != nil {
		t.Fatalf("Write manifest: %v", err)
	}
	if err := tapemanifest.Commit(snapshotDir); err != nil {
		t.Fatalf("Commit manifest: %v", err)
	}
	manifestBytes, err = os.ReadFile(filepath.Join(snapshotDir, "manifest.json"))
	if err != nil {
		t.Fatalf("Read manifest bytes: %v", err)
	}
	completeBytes, err = os.ReadFile(filepath.Join(snapshotDir, "complete.json"))
	if err != nil {
		t.Fatalf("Read completion bytes: %v", err)
	}
	return name, manifestBytes, completeBytes
}

func assertDroppedReason(
	t *testing.T,
	events []backupplan.Event,
	metadataRef string,
	generation int,
	reason backupplan.ItemReason,
) {
	t.Helper()
	assertEvent(
		t,
		events,
		backupplan.EventFreshnessChecked,
		func(event backupplan.Event) bool {
			for _, dropped := range event.Dropped {
				if dropped.MetadataRef == metadataRef &&
					dropped.Generation == generation &&
					dropped.Reason == string(reason) {
					return true
				}
			}
			return false
		},
	)
}

func assertNoDroppedReason(
	t *testing.T,
	events []backupplan.Event,
	metadataRef string,
	reason backupplan.ItemReason,
) {
	t.Helper()
	for _, event := range events {
		if event.Type != backupplan.EventFreshnessChecked {
			continue
		}
		for _, dropped := range event.Dropped {
			if dropped.MetadataRef == metadataRef &&
				dropped.Reason == string(reason) {
				t.Fatalf(
					"item %s unexpectedly dropped for %s: %#v",
					metadataRef,
					reason,
					event,
				)
			}
		}
	}
}

var _ executor.Drive = (*fixedCapacityDrive)(nil)

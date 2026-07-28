package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wyvernzora/kura/services/tape-backup/internal/fingerprint"
	"github.com/wyvernzora/kura/services/tape-backup/internal/seriesmeta"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapemanifest"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
)

var (
	// ErrPlanFailed reports an assertion that aborted one plan at its position.
	ErrPlanFailed = errors.New("executor: plan failed")
)

// ExistingSnapshot carries the exact manifest and completion-marker bytes for
// a snapshot that classification found already committed on the cartridge.
type ExistingSnapshot struct {
	MetadataRef string
	Generation  int
	Manifest    []byte
	Complete    []byte
}

// PreparedExecution contains phase-2 data that the later result hand-back
// slice needs. It does not publish or persist results.
type PreparedExecution struct {
	AlreadyPresent []ExistingSnapshot
}

// BackupActionRequest is the narrow hand-off to the byte-copy slice.
type BackupActionRequest struct {
	PlanID      string
	Cartridge   Cartridge
	LibraryRoot string
	Action      backupplan.Action
}

// BackupActionHandler executes one backup action after phase 2 accepts it.
// Byte copy, file progress, and committed-result collection belong to D2c-2.
type BackupActionHandler func(context.Context, BackupActionRequest) error

type actionDisposition uint8

const (
	actionPending actionDisposition = iota
	actionReady
	actionSkipped
	actionDropped
)

type preparedAction struct {
	action      backupplan.Action
	disposition actionDisposition
	reason      backupplan.ItemReason
	detail      string
}

// ExecutePreparedPlan classifies every backup target, pre-flights the remaining
// mutations, then executes the ordered assertions and accepted backup seams.
func ExecutePreparedPlan(
	ctx context.Context,
	prepared PreparedPlan,
	freeSpaceMargin int64,
	backup BackupActionHandler,
) (PreparedExecution, error) {
	if err := validatePreparedPlan(prepared, freeSpaceMargin, backup); err != nil {
		return PreparedExecution{}, err
	}

	actions := make([]preparedAction, len(prepared.Plan.Actions))
	for i, action := range prepared.Plan.Actions {
		actions[i].action = action
		if action.Type != backupplan.ActionBackup {
			continue
		}
		actions[i].disposition = actionReady
	}

	execution, err := classifyTargets(prepared, actions)
	if err != nil {
		return execution, planFailure(err)
	}
	if err := preflightBackups(prepared, actions, freeSpaceMargin); err != nil {
		return execution, planFailure(err)
	}
	if err := reportFreshness(prepared, actions); err != nil {
		return execution, planFailure(err)
	}
	if err := executeActions(ctx, prepared, actions, backup); err != nil {
		return execution, planFailure(err)
	}
	return execution, nil
}

func planFailure(err error) error {
	if errors.Is(err, ErrPlanFailed) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrPlanFailed, err)
}

func validatePreparedPlan(
	prepared PreparedPlan,
	freeSpaceMargin int64,
	backup BackupActionHandler,
) error {
	if prepared.Drive == nil {
		return errors.New("executor: prepared plan drive is required")
	}
	if prepared.Events == nil {
		return errors.New("executor: prepared plan event outbox is required")
	}
	if prepared.Cartridge.Root == "" {
		return errors.New("executor: prepared plan cartridge root is required")
	}
	if prepared.LibraryRoot == "" {
		return errors.New("executor: prepared plan library root is required")
	}
	if prepared.CommittedSnapshots == nil {
		return errors.New("executor: prepared plan committed set is required")
	}
	if freeSpaceMargin < 0 {
		return errors.New("executor: free space margin must not be negative")
	}
	if backup == nil {
		return errors.New("executor: backup action handler is required")
	}
	for _, action := range prepared.Plan.Actions {
		switch action.Type {
		case backupplan.ActionBackup,
			backupplan.ActionAssertVolume,
			backupplan.ActionAssertInventory,
			backupplan.ActionAssertFreeSpace:
		case backupplan.ActionReformat,
			backupplan.ActionAdmit,
			backupplan.ActionImport,
			backupplan.ActionVerify:
			return fmt.Errorf(
				"executor: plan %s contains out-of-scope action %q",
				prepared.Plan.PlanID,
				action.Type,
			)
		default:
			return fmt.Errorf(
				"executor: plan %s contains unknown action %q",
				prepared.Plan.PlanID,
				action.Type,
			)
		}
	}
	return nil
}

func classifyTargets(
	prepared PreparedPlan,
	actions []preparedAction,
) (PreparedExecution, error) {
	execution := PreparedExecution{
		AlreadyPresent: make([]ExistingSnapshot, 0),
	}
	for i := range actions {
		item := &actions[i]
		if item.action.Type != backupplan.ActionBackup {
			continue
		}
		name, err := tapevolume.SnapshotName(
			item.action.MetadataRef,
			item.action.Generation,
		)
		if err != nil {
			return PreparedExecution{}, fmt.Errorf(
				"executor: classify backup target: %w",
				err,
			)
		}
		if _, committed := prepared.CommittedSnapshots[name]; !committed {
			continue
		}

		snapshotDir := filepath.Join(tapevolume.SnapshotsDir(prepared.Cartridge.Root), name)
		if _, err := tapemanifest.Read(snapshotDir); err != nil {
			item.disposition = actionDropped
			item.reason = backupplan.ReasonChecksumMismatch
			item.detail = err.Error()
			if err := emitItemEvent(
				prepared,
				backupplan.EventItemFailed,
				*item,
			); err != nil {
				return PreparedExecution{}, err
			}
			continue
		}
		manifestBytes, err := os.ReadFile(filepath.Join(snapshotDir, "manifest.json"))
		if err != nil {
			return PreparedExecution{}, fmt.Errorf(
				"executor: read already-present manifest: %w",
				err,
			)
		}
		completeBytes, err := os.ReadFile(filepath.Join(snapshotDir, "complete.json"))
		if err != nil {
			return PreparedExecution{}, fmt.Errorf(
				"executor: read already-present completion marker: %w",
				err,
			)
		}
		item.disposition = actionSkipped
		item.reason = backupplan.ReasonAlreadyPresent
		execution.AlreadyPresent = append(
			execution.AlreadyPresent,
			ExistingSnapshot{
				MetadataRef: item.action.MetadataRef,
				Generation:  item.action.Generation,
				Manifest:    manifestBytes,
				Complete:    completeBytes,
			},
		)
		if err := emitItemEvent(
			prepared,
			backupplan.EventItemSkipped,
			*item,
		); err != nil {
			return PreparedExecution{}, err
		}
	}
	return execution, nil
}

func preflightBackups(
	prepared PreparedPlan,
	actions []preparedAction,
	freeSpaceMargin int64,
) error {
	ready := make([]int, 0)
	for i := range actions {
		item := &actions[i]
		if item.action.Type != backupplan.ActionBackup ||
			item.disposition != actionReady {
			continue
		}
		reason, detail := preflightBackup(prepared.LibraryRoot, item.action)
		if reason != "" {
			item.disposition = actionDropped
			item.reason = reason
			item.detail = detail
			if err := emitItemEvent(
				prepared,
				backupplan.EventItemFailed,
				*item,
			); err != nil {
				return err
			}
			continue
		}
		ready = append(ready, i)
	}
	if len(ready) == 0 {
		return nil
	}

	_, free, err := prepared.Drive.Capacity()
	if err != nil {
		return fmt.Errorf("executor: inspect capacity for pre-flight: %w", err)
	}
	available := free - freeSpaceMargin
	var used int64
	for position, index := range ready {
		bytes := actions[index].action.Bytes
		if available < 0 || bytes > available-used {
			for _, trailing := range ready[position:] {
				item := &actions[trailing]
				item.disposition = actionDropped
				item.reason = backupplan.ReasonInsufficientSpace
				item.detail = fmt.Sprintf(
					"trailing item needs %d bytes; %d bytes used, %d-byte margin, %d bytes free",
					item.action.Bytes,
					used,
					freeSpaceMargin,
					free,
				)
				if err := emitItemEvent(
					prepared,
					backupplan.EventItemFailed,
					*item,
				); err != nil {
					return err
				}
			}
			break
		}
		used += bytes
	}
	return nil
}

func preflightBackup(
	libraryRoot string,
	action backupplan.Action,
) (reason backupplan.ItemReason, detail string) {
	seriesRoot := filepath.Join(libraryRoot, filepath.FromSlash(action.RootPath))
	info, err := os.Lstat(seriesRoot)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return backupplan.ReasonSeriesRootMissing, err.Error()
	case err != nil:
		return backupplan.ReasonSeriesMoved, err.Error()
	case !info.IsDir() || info.Mode()&os.ModeSymlink != 0:
		return backupplan.ReasonSeriesMoved, "series root is not a real directory"
	}

	metadata, err := seriesmeta.Read(filepath.Join(seriesRoot, ".kura", "series.json"))
	if err != nil {
		return backupplan.ReasonSeriesMoved, err.Error()
	}
	if metadata.MetadataRef != action.MetadataRef {
		return backupplan.ReasonSeriesMoved, fmt.Sprintf(
			"metadataRef is %q, want %q",
			metadata.MetadataRef,
			action.MetadataRef,
		)
	}
	if metadata.Generation != action.Generation {
		return backupplan.ReasonSeriesMoved, fmt.Sprintf(
			"generation is %d, want %d",
			metadata.Generation,
			action.Generation,
		)
	}

	actual, err := fingerprint.ComputeExcludingKura(seriesRoot)
	if err != nil {
		if errors.Is(err, fingerprint.ErrSymlink) ||
			errors.Is(err, fingerprint.ErrNonRegularFile) {
			return backupplan.ReasonUnsupportedFileType, err.Error()
		}
		return backupplan.ReasonPayloadDrift, err.Error()
	}
	if string(actual) != action.PayloadFingerprint {
		return backupplan.ReasonPayloadDrift, fmt.Sprintf(
			"payload fingerprint is %q, want %q",
			actual,
			action.PayloadFingerprint,
		)
	}
	if metadata.HasStagedIntent {
		return backupplan.ReasonStagedIntent, "series has staged intent"
	}
	if metadata.HasActiveClaim {
		return backupplan.ReasonClaimStale, "series has an active claim"
	}
	return "", ""
}

func reportFreshness(
	prepared PreparedPlan,
	actions []preparedAction,
) error {
	dropped := make([]backupplan.DroppedItem, 0)
	for _, item := range actions {
		if item.action.Type != backupplan.ActionBackup ||
			item.disposition != actionDropped {
			continue
		}
		dropped = append(dropped, backupplan.DroppedItem{
			MetadataRef: item.action.MetadataRef,
			Generation:  item.action.Generation,
			Reason:      string(item.reason),
		})
	}
	return prepared.Events.Emit(backupplan.Event{
		Type:    backupplan.EventFreshnessChecked,
		PlanID:  prepared.Plan.PlanID,
		Dropped: dropped,
	})
}

func executeActions(
	ctx context.Context,
	prepared PreparedPlan,
	actions []preparedAction,
	backup BackupActionHandler,
) error {
	itemsWritten := 0
	itemsFailed := 0
	for _, item := range actions {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch item.action.Type {
		case backupplan.ActionAssertVolume:
			if err := assertVolume(prepared, item.action); err != nil {
				return err
			}
		case backupplan.ActionAssertInventory:
			if err := assertInventory(prepared, actions, item.action); err != nil {
				return err
			}
		case backupplan.ActionAssertFreeSpace:
			if err := assertFreeSpace(prepared, item.action); err != nil {
				return err
			}
		case backupplan.ActionBackup:
			written, failed, err := executeBackup(ctx, prepared, item, backup)
			if err != nil {
				return err
			}
			if written {
				itemsWritten++
			}
			if failed {
				itemsFailed++
			}
		default:
			return fmt.Errorf(
				"executor: execute unsupported action %q",
				item.action.Type,
			)
		}
	}
	return prepared.Events.Emit(backupplan.Event{
		Type:         backupplan.EventPlanCompleted,
		PlanID:       prepared.Plan.PlanID,
		ItemsWritten: itemsWritten,
		ItemsFailed:  itemsFailed,
	})
}

func executeBackup(
	ctx context.Context,
	prepared PreparedPlan,
	item preparedAction,
	backup BackupActionHandler,
) (written, failed bool, err error) {
	switch item.disposition {
	case actionReady:
		if err := backup(ctx, BackupActionRequest{
			PlanID:      prepared.Plan.PlanID,
			Cartridge:   prepared.Cartridge,
			LibraryRoot: prepared.LibraryRoot,
			Action:      item.action,
		}); err != nil {
			return false, false, fmt.Errorf(
				"executor: execute backup action (%q, %d): %w",
				item.action.MetadataRef,
				item.action.Generation,
				err,
			)
		}
		return true, false, nil
	case actionDropped:
		return false, true, nil
	case actionSkipped:
		return false, false, nil
	default:
		return false, false, errors.New("executor: backup action was not classified")
	}
}

func assertVolume(prepared PreparedPlan, action backupplan.Action) error {
	mounted, err := tapevolume.Read(prepared.Cartridge.Root)
	if err != nil {
		return failAssertion(
			prepared,
			action.Type,
			fmt.Sprintf("read mounted volume header: %v", err),
		)
	}
	if mounted.VolumeID != action.VolumeID {
		return failAssertion(
			prepared,
			action.Type,
			fmt.Sprintf(
				"mounted volume is %s, expected %s",
				mounted.VolumeID,
				action.VolumeID,
			),
		)
	}
	return nil
}

func assertInventory(
	prepared PreparedPlan,
	actions []preparedAction,
	action backupplan.Action,
) error {
	expected := make(map[string]struct{}, len(action.Snapshots))
	excludedExtras := make(map[string]struct{})
	for _, name := range action.Snapshots {
		expected[name] = struct{}{}
	}
	for _, item := range actions {
		if item.action.Type != backupplan.ActionBackup ||
			item.disposition != actionDropped {
			continue
		}
		name, err := tapevolume.SnapshotName(
			item.action.MetadataRef,
			item.action.Generation,
		)
		if err != nil {
			return fmt.Errorf("executor: adjust inventory assertion: %w", err)
		}
		delete(expected, name)
		if item.reason == backupplan.ReasonChecksumMismatch {
			excludedExtras[name] = struct{}{}
		}
	}

	present, err := committedMarkers(prepared.Cartridge.Root)
	if err != nil {
		return failAssertion(prepared, action.Type, err.Error())
	}
	for name := range excludedExtras {
		delete(present, name)
	}
	missing := setDifference(expected, present)
	extra := setDifference(present, expected)
	result := "match"
	if len(missing) > 0 {
		result = "missing"
	}
	if err := prepared.Events.Emit(backupplan.Event{
		Type:           backupplan.EventDivergenceChecked,
		PlanID:         prepared.Plan.PlanID,
		Result:         result,
		ExtraSnapshots: extra,
	}); err != nil {
		return err
	}
	if len(missing) > 0 {
		return failAssertion(
			prepared,
			action.Type,
			"missing snapshots: "+strings.Join(missing, ", "),
		)
	}
	return nil
}

func assertFreeSpace(prepared PreparedPlan, action backupplan.Action) error {
	_, free, err := prepared.Drive.Capacity()
	if err != nil {
		return failAssertion(
			prepared,
			action.Type,
			fmt.Sprintf("read free space: %v", err),
		)
	}
	if free < action.Bytes {
		return failAssertion(
			prepared,
			action.Type,
			fmt.Sprintf("free space is %d bytes, expected at least %d", free, action.Bytes),
		)
	}
	return nil
}

func failAssertion(
	prepared PreparedPlan,
	actionType backupplan.ActionType,
	detail string,
) error {
	if err := prepared.Events.Emit(backupplan.Event{
		Type:   backupplan.EventPlanFailed,
		PlanID: prepared.Plan.PlanID,
		Reason: string(actionType),
		Detail: detail,
	}); err != nil {
		return err
	}
	return fmt.Errorf(
		"%w: plan %s %s: %s",
		ErrPlanFailed,
		prepared.Plan.PlanID,
		actionType,
		detail,
	)
}

func committedMarkers(ltfsRoot string) (map[string]struct{}, error) {
	present := make(map[string]struct{})
	entries, err := os.ReadDir(tapevolume.SnapshotsDir(ltfsRoot))
	if errors.Is(err, os.ErrNotExist) {
		return present, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read snapshots directory: %w", err)
	}
	for _, entry := range entries {
		entryPath := filepath.Join(tapevolume.SnapshotsDir(ltfsRoot), entry.Name())
		entryInfo, err := os.Lstat(entryPath)
		if err != nil {
			return nil, fmt.Errorf("inspect snapshot %q: %w", entry.Name(), err)
		}
		if !entryInfo.IsDir() || entryInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		markerInfo, err := os.Lstat(filepath.Join(entryPath, "complete.json"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf(
				"inspect snapshot %q completion marker: %w",
				entry.Name(),
				err,
			)
		}
		if markerInfo.Mode().IsRegular() &&
			markerInfo.Mode()&os.ModeSymlink == 0 {
			present[entry.Name()] = struct{}{}
		}
	}
	return present, nil
}

func setDifference(
	left, right map[string]struct{},
) []string {
	difference := make([]string, 0)
	for name := range left {
		if _, exists := right[name]; !exists {
			difference = append(difference, name)
		}
	}
	slices.Sort(difference)
	return difference
}

func emitItemEvent(
	prepared PreparedPlan,
	eventType backupplan.EventType,
	item preparedAction,
) error {
	return prepared.Events.Emit(backupplan.Event{
		Type:        eventType,
		PlanID:      prepared.Plan.PlanID,
		MetadataRef: item.action.MetadataRef,
		Generation:  item.action.Generation,
		Reason:      string(item.reason),
		Detail:      item.detail,
	})
}

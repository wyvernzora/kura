package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/fingerprint"
	"github.com/wyvernzora/kura/services/tape-backup/internal/seriesmeta"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapecatalog"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
)

var (
	// ErrPlanFailed reports a failure that halted and retired one plan.
	ErrPlanFailed = errors.New("executor: plan failed")
)

// SnapshotResult carries exact committed manifest and completion-marker bytes.
type SnapshotResult struct {
	MetadataRef string
	Generation  int
	Manifest    []byte
	Complete    []byte
}

// BackupActionRequest is the narrow hand-off to the byte-copy path.
type BackupActionRequest struct {
	PlanID                  string
	Cartridge               Cartridge
	Journal                 *backupplan.Writer
	LibraryRoot             string
	WrittenBy               backupplan.Creator
	Action                  backupplan.Action
	AllowCommittedOverwrite bool
	result                  *SnapshotResult
}

// BackupActionHandler copies one backup action through its completion marker.
type BackupActionHandler func(context.Context, BackupActionRequest) error

// ExecutePreparedPlan runs one validated fill plan. Every snapshot batch is
// synced before its catalog records are appended.
func ExecutePreparedPlan(
	ctx context.Context,
	prepared PreparedPlan,
	freeSpaceMargin int64,
	flushCadence int,
	backup BackupActionHandler,
) error {
	if err := validatePreparedPlan(
		prepared,
		freeSpaceMargin,
		flushCadence,
		backup,
	); err != nil {
		return err
	}

	leadingInventoryIndex, err := findLeadingInventory(prepared.Plan)
	if err != nil {
		return planFailure(err)
	}
	execution := fillExecution{
		prepared:              prepared,
		freeSpaceMargin:       freeSpaceMargin,
		flushCadence:          flushCadence,
		backup:                backup,
		leadingInventoryIndex: leadingInventoryIndex,
		pending:               make([]SnapshotResult, 0, flushCadence),
	}
	if err := execution.run(ctx); err != nil {
		return planFailure(err)
	}
	return appendSessionEvent(prepared.Journal, backupplan.Event{
		Type:         backupplan.EventPlanCompleted,
		PlanID:       prepared.Plan.PlanID,
		ItemsWritten: execution.itemsWritten,
		ItemsFailed:  0,
	})
}

type fillExecution struct {
	prepared              PreparedPlan
	freeSpaceMargin       int64
	flushCadence          int
	backup                BackupActionHandler
	leadingInventoryIndex int
	pending               []SnapshotResult
	itemsWritten          int
	seenBackup            bool
}

func (e *fillExecution) run(ctx context.Context) error {
	for index, action := range e.prepared.Plan.Actions {
		if err := ctx.Err(); err != nil {
			return err
		}
		if action.Type != backupplan.ActionBackup {
			if err := e.flushPending(); err != nil {
				return err
			}
		}
		if err := e.executeAction(ctx, index, action); err != nil {
			return err
		}
	}
	return e.flushPending()
}

func (e *fillExecution) executeAction(
	ctx context.Context,
	index int,
	action backupplan.Action,
) error {
	switch action.Type {
	case backupplan.ActionAssertVolume:
		if !e.seenBackup {
			return nil
		}
		return assertVolume(e.prepared, action)
	case backupplan.ActionAssertInventory:
		if index == e.leadingInventoryIndex {
			return nil
		}
		return assertInventory(e.prepared, action)
	case backupplan.ActionAssertFreeSpace:
		return assertFreeSpace(e.prepared, action)
	case backupplan.ActionBackup:
		e.seenBackup = true
		result, skipped, err := executeBackup(
			ctx,
			e.prepared,
			action,
			e.freeSpaceMargin,
			e.backup,
		)
		if err != nil || skipped {
			return err
		}
		e.pending = append(e.pending, result)
		if len(e.pending) < e.flushCadence {
			return nil
		}
		return e.flushPending()
	default:
		return fmt.Errorf(
			"executor: execute unsupported action %q",
			action.Type,
		)
	}
}

func (e *fillExecution) flushPending() error {
	if len(e.pending) == 0 {
		return nil
	}
	if err := flushSnapshots(e.prepared, e.pending); err != nil {
		return err
	}
	e.itemsWritten += len(e.pending)
	e.pending = e.pending[:0]
	return nil
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
	flushCadence int,
	backup BackupActionHandler,
) error {
	if prepared.Drive == nil {
		return errors.New("executor: prepared plan drive is required")
	}
	if prepared.Journal == nil {
		return errors.New("executor: prepared plan session journal is required")
	}
	if prepared.Cartridge.Root == "" {
		return errors.New("executor: prepared plan cartridge root is required")
	}
	if prepared.StateRoot == "" {
		return errors.New("executor: prepared plan state root is required")
	}
	if prepared.LibraryRoot == "" {
		return errors.New("executor: prepared plan library root is required")
	}
	if prepared.CatalogSnapshots == nil {
		return errors.New("executor: prepared plan catalog set is required")
	}
	if prepared.DebrisSnapshots == nil {
		return errors.New("executor: prepared plan debris set is required")
	}
	if freeSpaceMargin < 0 {
		return errors.New("executor: free space margin must not be negative")
	}
	if flushCadence < 1 {
		return errors.New("executor: flush cadence must be at least 1")
	}
	if backup == nil {
		return errors.New("executor: backup action handler is required")
	}
	return validateExecutionActionTypes(prepared.Plan)
}

func validateExecutionActionTypes(plan backupplan.Plan) error {
	for _, action := range plan.Actions {
		switch action.Type {
		case backupplan.ActionBackup,
			backupplan.ActionAssertVolume,
			backupplan.ActionAssertInventory,
			backupplan.ActionAssertFreeSpace:
			continue
		case backupplan.ActionReformat, backupplan.ActionAdmit:
			return fmt.Errorf(
				"executor: plan %s contains init action %q",
				plan.PlanID,
				action.Type,
			)
		case backupplan.ActionImport, backupplan.ActionVerify:
			return fmt.Errorf(
				"executor: plan %s contains deferred action %q",
				plan.PlanID,
				action.Type,
			)
		default:
			return fmt.Errorf(
				"executor: plan %s contains unknown action %q",
				plan.PlanID,
				action.Type,
			)
		}
	}
	return nil
}

func findLeadingInventory(plan backupplan.Plan) (int, error) {
	for index, action := range plan.Actions {
		if action.Type == backupplan.ActionBackup {
			break
		}
		if action.Type == backupplan.ActionAssertInventory {
			return index, nil
		}
	}
	return -1, fmt.Errorf(
		"executor: plan %s must assert inventory before backup",
		plan.PlanID,
	)
}

func executeBackup(
	ctx context.Context,
	prepared PreparedPlan,
	action backupplan.Action,
	freeSpaceMargin int64,
	backup BackupActionHandler,
) (SnapshotResult, bool, error) {
	name, err := tapevolume.SnapshotName(action.MetadataRef, action.Generation)
	if err != nil {
		return SnapshotResult{}, false, fmt.Errorf(
			"executor: classify backup target: %w",
			err,
		)
	}
	if _, cataloged := prepared.CatalogSnapshots[name]; cataloged {
		if err := appendSessionEvent(prepared.Journal, backupplan.Event{
			Type:        backupplan.EventItemSkipped,
			PlanID:      prepared.Plan.PlanID,
			MetadataRef: action.MetadataRef,
			Generation:  action.Generation,
			Reason:      string(backupplan.ReasonAlreadyPresent),
		}); err != nil {
			return SnapshotResult{}, false, err
		}
		return SnapshotResult{}, true, nil
	}

	if reason, detail := preflightBackup(prepared.LibraryRoot, action); reason != "" {
		return SnapshotResult{}, false, haltItem(prepared, action, reason, detail)
	}
	_, free, err := prepared.Drive.Capacity()
	if err != nil {
		return SnapshotResult{}, false, haltItem(
			prepared,
			action,
			backupplan.ReasonWriteFailed,
			fmt.Sprintf("inspect capacity: %v", err),
		)
	}
	available := free - freeSpaceMargin
	if available < 0 || action.Bytes > available {
		return SnapshotResult{}, false, haltItem(
			prepared,
			action,
			backupplan.ReasonInsufficientSpace,
			fmt.Sprintf(
				"item needs %d bytes; %d-byte margin, %d bytes free",
				action.Bytes,
				freeSpaceMargin,
				free,
			),
		)
	}

	result := SnapshotResult{}
	_, overwriteDebris := prepared.DebrisSnapshots[name]
	err = backup(ctx, BackupActionRequest{
		PlanID:                  prepared.Plan.PlanID,
		Cartridge:               prepared.Cartridge,
		Journal:                 prepared.Journal,
		LibraryRoot:             prepared.LibraryRoot,
		WrittenBy:               prepared.Plan.CreatedBy,
		Action:                  action,
		AllowCommittedOverwrite: overwriteDebris,
		result:                  &result,
	})
	if err == nil {
		return result, false, nil
	}
	failure, ok := errors.AsType[*itemFailure](err)
	if ok {
		haltErr := haltItem(
			prepared,
			action,
			failure.reason,
			failure.Error(),
		)
		return SnapshotResult{}, false, errors.Join(haltErr, err)
	}
	haltErr := haltItem(
		prepared,
		action,
		backupplan.ReasonWriteFailed,
		err.Error(),
	)
	return SnapshotResult{}, false, errors.Join(haltErr, err)
}

func haltItem(
	prepared PreparedPlan,
	action backupplan.Action,
	reason backupplan.ItemReason,
	detail string,
) error {
	if err := appendSessionEvent(prepared.Journal, backupplan.Event{
		Type:        backupplan.EventItemFailed,
		PlanID:      prepared.Plan.PlanID,
		MetadataRef: action.MetadataRef,
		Generation:  action.Generation,
		Reason:      string(reason),
		Detail:      detail,
	}); err != nil {
		return err
	}
	return fmt.Errorf(
		"executor: backup action (%q, %d) failed: %s: %s",
		action.MetadataRef,
		action.Generation,
		reason,
		detail,
	)
}

func flushSnapshots(
	prepared PreparedPlan,
	results []SnapshotResult,
) error {
	if err := prepared.Drive.Sync(); err != nil {
		return fmt.Errorf("executor: sync cartridge index: %w", err)
	}
	for _, result := range results {
		name, err := tapevolume.SnapshotName(
			result.MetadataRef,
			result.Generation,
		)
		if err != nil {
			return fmt.Errorf("executor: name committed snapshot: %w", err)
		}
		if err := tapecatalog.PutSnapshot(
			prepared.StateRoot,
			prepared.Volume.VolumeID,
			name,
			result.Manifest,
			result.Complete,
		); err != nil {
			return fmt.Errorf("executor: append catalog snapshot %q: %w", name, err)
		}
		prepared.CatalogSnapshots[name] = struct{}{}
		if err := appendSessionEvent(prepared.Journal, backupplan.Event{
			Type:        backupplan.EventItemCompleted,
			PlanID:      prepared.Plan.PlanID,
			MetadataRef: result.MetadataRef,
			Generation:  result.Generation,
			Snapshot:    name,
		}); err != nil {
			return err
		}
	}
	total, free, err := prepared.Drive.Capacity()
	if err != nil {
		return fmt.Errorf("executor: observe capacity after sync: %w", err)
	}
	observed := prepared.CatalogObservation
	observed.ObservedAt = time.Now().UTC().Truncate(time.Second)
	observed.CapacityBytes = total
	observed.FreeBytes = free
	if err := tapecatalog.SaveObserved(
		prepared.StateRoot,
		prepared.Volume.VolumeID,
		observed,
	); err != nil {
		return fmt.Errorf("executor: refresh catalog observation: %w", err)
	}
	prepared.CatalogObservation = observed
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
	action backupplan.Action,
) error {
	expected := make(map[string]struct{}, len(action.Snapshots))
	for _, name := range action.Snapshots {
		expected[name] = struct{}{}
	}
	present, err := committedMarkers(prepared.Cartridge.Root)
	if err != nil {
		return failAssertion(prepared, action.Type, err.Error())
	}
	difference := compareInventory(expected, present)
	result := "match"
	if len(difference.missing) > 0 {
		result = "missing"
	}
	if err := appendSessionEvent(prepared.Journal, backupplan.Event{
		Type:           backupplan.EventDivergenceChecked,
		PlanID:         prepared.Plan.PlanID,
		Result:         result,
		ExtraSnapshots: difference.extra,
	}); err != nil {
		return err
	}
	if len(difference.missing) > 0 {
		return failAssertion(
			prepared,
			action.Type,
			"missing snapshots: "+strings.Join(difference.missing, ", "),
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
			fmt.Sprintf(
				"free space is %d bytes, expected at least %d",
				free,
				action.Bytes,
			),
		)
	}
	return nil
}

func failAssertion(
	prepared PreparedPlan,
	actionType backupplan.ActionType,
	detail string,
) error {
	if err := appendSessionEvent(prepared.Journal, backupplan.Event{
		Type:   backupplan.EventPlanFailed,
		PlanID: prepared.Plan.PlanID,
		Reason: string(actionType),
		Detail: detail,
	}); err != nil {
		return err
	}
	return fmt.Errorf(
		"executor: plan %s %s: %s",
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

func setDifference(left, right map[string]struct{}) []string {
	difference := make([]string, 0)
	for name := range left {
		if _, exists := right[name]; !exists {
			difference = append(difference, name)
		}
	}
	slices.Sort(difference)
	return difference
}

type inventoryDifference struct {
	missing []string
	extra   []string
}

func compareInventory(
	expected, present map[string]struct{},
) inventoryDifference {
	return inventoryDifference{
		missing: setDifference(expected, present),
		extra:   setDifference(present, expected),
	}
}

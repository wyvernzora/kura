package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/seriesmeta"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapecatalog"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapemanifest"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

var (
	// ErrNoReadyPlans reports a session started without work.
	ErrNoReadyPlans = errors.New("executor: no ready plans")
	// ErrEncryptionInactive reports that the write gate rejected a cartridge
	// because drive encryption was inactive.
	ErrEncryptionInactive = errors.New("executor: drive encryption is inactive")
	// ErrSessionActive reports that another session owns the service slot.
	ErrSessionActive = errors.New("executor: session is already active")
)

var sessionRegistry struct {
	sync.Mutex
	active bool
}

// PreparedPlan is one fill plan whose loaded cartridge and recorded pre-state
// have been validated. DebrisSnapshots is forensic session state only.
type PreparedPlan struct {
	Drive              Drive
	Journal            *backupplan.Writer
	Cartridge          Cartridge
	Volume             tapevolume.Volume
	Plan               backupplan.Plan
	StateRoot          string
	LibraryRoot        string
	CatalogSnapshots   map[string]struct{}
	DebrisSnapshots    map[string]struct{}
	CatalogObservation tapecatalog.Observed
}

// SessionOptions controls the polling loop and per-snapshot commit policy.
type SessionOptions struct {
	PollInterval    time.Duration
	IdleTimeout     time.Duration
	StateRoot       string
	LibraryRoot     string
	FreeSpaceMargin int64
	FlushCadence    int
}

type admittedPlan struct {
	plan backupplan.Plan
	pin  volume.ID
}

// RunSession executes ready fill plans synchronously as cartridges are swapped.
// Each plan leaves the live set: success completes it and any execution failure
// discards it after recording the halt.
func RunSession(
	ctx context.Context,
	drive Drive,
	plans []backupplan.Plan,
	journal *backupplan.Writer,
	backup BackupActionHandler,
	options SessionOptions,
) (resultErr error) {
	if err := validateRunSession(drive, journal, backup, options); err != nil {
		return err
	}
	pending, err := admitPlans(options.StateRoot, plans)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return ErrNoReadyPlans
	}
	if err := registerSession(); err != nil {
		return err
	}
	defer unregisterSession()
	if err := drive.Open(); err != nil {
		return fmt.Errorf("executor: open drive: %w", err)
	}
	defer func() {
		if err := drive.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("executor: close drive: %w", err))
		}
	}()

	return runAdmittedPlans(ctx, drive, pending, journal, backup, options)
}

func registerSession() error {
	sessionRegistry.Lock()
	defer sessionRegistry.Unlock()
	if sessionRegistry.active {
		return ErrSessionActive
	}
	sessionRegistry.active = true
	return nil
}

func unregisterSession() {
	sessionRegistry.Lock()
	defer sessionRegistry.Unlock()
	sessionRegistry.active = false
}

func runAdmittedPlans(
	ctx context.Context,
	drive Drive,
	pending []admittedPlan,
	journal *backupplan.Writer,
	backup BackupActionHandler,
	options SessionOptions,
) error {
	plansAdmitted := len(pending)
	plansCompleted := 0
	idleDeadline := time.Now().Add(options.IdleTimeout)
	for len(pending) > 0 {
		if err := appendSessionEvent(journal, backupplan.Event{
			Type:       backupplan.EventWaitingForTape,
			Candidates: candidateTapeIDs(pending),
		}); err != nil {
			return err
		}

		index, identity, err := waitForCandidate(
			ctx,
			drive,
			pending,
			options.PollInterval,
			idleDeadline,
		)
		if errors.Is(err, errIdleTimeout) {
			retireErr := retirePendingPlans(
				options.StateRoot,
				journal,
				pending,
				errors.New("executor: session idle timeout"),
			)
			return errors.Join(
				retireErr,
				appendTerminal(journal, "idle_timeout", plansCompleted),
			)
		}
		if err != nil {
			retireErr := retirePendingPlans(
				options.StateRoot,
				journal,
				pending,
				err,
			)
			terminalErr := appendTerminal(journal, "session_failed", plansCompleted)
			return errors.Join(err, retireErr, terminalErr)
		}

		selected := pending[index]
		prepared, err := prepareFillCartridge(
			drive,
			identity,
			selected,
			journal,
			options,
		)
		if err == nil {
			err = ExecutePreparedPlan(
				ctx,
				prepared,
				options.FreeSpaceMargin,
				options.FlushCadence,
				backup,
			)
		}
		if err != nil {
			if retireErr := retirePlan(
				options.StateRoot,
				journal,
				selected.plan,
				err,
			); retireErr != nil {
				return errors.Join(err, retireErr)
			}
			pending = append(pending[:index], pending[index+1:]...)
			idleDeadline = time.Now().Add(options.IdleTimeout)
			continue
		}
		if err := backupplan.Complete(options.StateRoot, selected.plan.PlanID); err != nil {
			return fmt.Errorf(
				"executor: complete plan %s: %w",
				selected.plan.PlanID,
				err,
			)
		}
		pending = append(pending[:index], pending[index+1:]...)
		plansCompleted++
		idleDeadline = time.Now().Add(options.IdleTimeout)
	}

	reason := "completed"
	if plansCompleted < plansAdmitted {
		reason = "plans_failed"
	}
	return appendTerminal(journal, reason, plansCompleted)
}

func validateRunSession(
	drive Drive,
	journal *backupplan.Writer,
	backup BackupActionHandler,
	options SessionOptions,
) error {
	if drive == nil {
		return errors.New("executor: drive is required")
	}
	if journal == nil {
		return errors.New("executor: session journal is required")
	}
	if backup == nil {
		return errors.New("executor: backup action handler is required")
	}
	if options.PollInterval <= 0 {
		return errors.New("executor: poll interval must be greater than zero")
	}
	if options.IdleTimeout <= 0 {
		return errors.New("executor: idle timeout must be greater than zero")
	}
	if options.StateRoot == "" {
		return errors.New("executor: state root is required")
	}
	if options.LibraryRoot == "" {
		return errors.New("executor: library root is required")
	}
	if options.FreeSpaceMargin < 0 {
		return errors.New("executor: free space margin must not be negative")
	}
	if options.FlushCadence < 1 {
		return errors.New("executor: flush cadence must be at least 1")
	}
	return nil
}

var errIdleTimeout = errors.New("idle timeout")

func waitForCandidate(
	ctx context.Context,
	drive Drive,
	plans []admittedPlan,
	pollInterval time.Duration,
	idleDeadline time.Time,
) (int, LoadedIdentity, error) {
	for {
		identity, err := drive.LoadedIdentity()
		switch {
		case err != nil:
			return -1, LoadedIdentity{}, fmt.Errorf(
				"executor: identify loaded cartridge: %w",
				err,
			)
		case identity.State == LoadedIdentityIdentified:
			if index := matchingPlan(plans, identity.Cartridge.TapeID); index >= 0 {
				if _, _, capacityErr := drive.Capacity(); capacityErr == nil {
					return index, identity, nil
				} else if !errors.Is(capacityErr, ErrNotMounted) {
					return -1, LoadedIdentity{}, fmt.Errorf(
						"executor: inspect mounted cartridge capacity: %w",
						capacityErr,
					)
				}
			}
		case identity.State == LoadedIdentityNone,
			identity.State == LoadedIdentityUnidentified:
		default:
			return -1, LoadedIdentity{}, fmt.Errorf(
				"executor: identify loaded cartridge: unknown identity state %q",
				identity.State,
			)
		}

		remaining := time.Until(idleDeadline)
		if remaining <= 0 {
			return -1, LoadedIdentity{}, errIdleTimeout
		}
		timer := time.NewTimer(min(pollInterval, remaining))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return -1, LoadedIdentity{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func prepareFillCartridge(
	drive Drive,
	identity LoadedIdentity,
	admitted admittedPlan,
	journal *backupplan.Writer,
	options SessionOptions,
) (PreparedPlan, error) {
	plan := admitted.plan
	if err := appendSessionEvent(journal, backupplan.Event{
		Type:   backupplan.EventPlanStarted,
		PlanID: plan.PlanID,
	}); err != nil {
		return PreparedPlan{}, err
	}
	if err := verifyFillEncryption(drive, journal, plan.PlanID); err != nil {
		return PreparedPlan{}, err
	}

	entry, err := loadCatalogEntry(options.StateRoot, admitted.pin)
	if err != nil {
		return PreparedPlan{}, fmt.Errorf(
			"executor: plan %s catalog entry is not complete: %w",
			plan.PlanID,
			err,
		)
	}
	if err := validateFillIdentity(identity, admitted, entry); err != nil {
		return PreparedPlan{}, err
	}
	if err := validateRecordedInventory(plan, entry.snapshots); err != nil {
		return PreparedPlan{}, err
	}
	debris, err := observeFillTape(identity, entry, journal, options, plan)
	if err != nil {
		return PreparedPlan{}, err
	}
	if err := appendSessionEvent(journal, backupplan.Event{
		Type:     backupplan.EventTapeLoaded,
		TapeID:   identity.Volume.TapeID,
		VolumeID: identity.Volume.VolumeID,
	}); err != nil {
		return PreparedPlan{}, err
	}

	return PreparedPlan{
		Drive:              drive,
		Journal:            journal,
		Cartridge:          identity.Cartridge,
		Volume:             identity.Volume,
		Plan:               plan,
		StateRoot:          options.StateRoot,
		LibraryRoot:        options.LibraryRoot,
		CatalogSnapshots:   entry.snapshots,
		DebrisSnapshots:    debris,
		CatalogObservation: entry.observed,
	}, nil
}

func verifyFillEncryption(
	drive Drive,
	journal *backupplan.Writer,
	planID string,
) error {
	active, err := drive.EncryptionActive()
	if err != nil {
		return fmt.Errorf("executor: check drive encryption: %w", err)
	}
	if !active {
		return ErrEncryptionInactive
	}
	return appendSessionEvent(journal, backupplan.Event{
		Type:   backupplan.EventEncryptionVerified,
		PlanID: planID,
	})
}

func validateFillIdentity(
	identity LoadedIdentity,
	admitted admittedPlan,
	entry catalogEntry,
) error {
	plan := admitted.plan
	matchesVolume := identity.Volume.VolumeID == admitted.pin &&
		entry.volume.VolumeID == admitted.pin
	matchesTape := identity.Volume.TapeID == plan.Target.TapeID &&
		entry.volume.TapeID == plan.Target.TapeID
	if matchesVolume && matchesTape {
		return nil
	}
	return fmt.Errorf(
		"executor: plan %s pre-state mismatch: loaded volume %s on tape %s, catalog volume %s on tape %s, want volume %s on tape %s",
		plan.PlanID,
		identity.Volume.VolumeID,
		identity.Volume.TapeID,
		entry.volume.VolumeID,
		entry.volume.TapeID,
		admitted.pin,
		plan.Target.TapeID,
	)
}

func validateRecordedInventory(
	plan backupplan.Plan,
	catalogSnapshots map[string]struct{},
) error {
	expected, err := leadingInventory(plan)
	if err != nil {
		return err
	}
	catalogNames := sortedSet(catalogSnapshots)
	expectedNames := sortedSet(expected)
	if reflect.DeepEqual(expectedNames, catalogNames) {
		return nil
	}
	return fmt.Errorf(
		"executor: plan %s pre-state mismatch: catalog snapshots %v, plan expects %v",
		plan.PlanID,
		catalogNames,
		expectedNames,
	)
}

func observeFillTape(
	identity LoadedIdentity,
	entry catalogEntry,
	journal *backupplan.Writer,
	options SessionOptions,
	plan backupplan.Plan,
) (map[string]struct{}, error) {
	appendEvent := func(event backupplan.Event) error {
		return appendSessionEvent(journal, event)
	}
	tapeSnapshots, err := sweepSnapshots(identity.Cartridge.Root, appendEvent)
	if err != nil {
		return nil, err
	}
	difference := compareInventory(entry.snapshots, tapeSnapshots)
	if len(difference.missing) > 0 {
		return nil, fmt.Errorf(
			"executor: plan %s pre-state mismatch: catalog snapshots missing from tape: %s",
			plan.PlanID,
			strings.Join(difference.missing, ", "),
		)
	}
	indexed, err := libraryMetadataRefs(options.LibraryRoot)
	if err != nil {
		return nil, fmt.Errorf(
			"executor: inspect library index for pre-state validation: %w",
			err,
		)
	}
	debris, foreign, err := classifyTapeExtras(
		difference.extra,
		indexed,
	)
	if err != nil {
		return nil, err
	}
	if len(foreign) > 0 {
		return nil, fmt.Errorf(
			"executor: plan %s divergence_deferred: foreign marked snapshots: %s",
			plan.PlanID,
			strings.Join(foreign, ", "),
		)
	}
	if err := appendSessionEvent(journal, backupplan.Event{
		Type:           backupplan.EventDivergenceChecked,
		PlanID:         plan.PlanID,
		Result:         "match",
		ExtraSnapshots: sortedSet(debris),
	}); err != nil {
		return nil, err
	}
	return debris, nil
}

func classifyTapeExtras(
	extras []string,
	indexed map[string]struct{},
) (
	debris map[string]struct{},
	foreign []string,
	err error,
) {
	debris = make(map[string]struct{})
	foreign = make([]string, 0)
	for _, name := range extras {
		metadataRef, _, err := tapevolume.ParseSnapshotName(name)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"executor: parse swept snapshot %q: %w",
				name,
				err,
			)
		}
		if _, exists := indexed[metadataRef]; exists {
			debris[name] = struct{}{}
			continue
		}
		foreign = append(foreign, name)
	}
	slices.Sort(foreign)
	return debris, foreign, nil
}

type catalogEntry struct {
	volume    tapevolume.Volume
	observed  tapecatalog.Observed
	snapshots map[string]struct{}
}

func loadCatalogEntry(stateRoot string, id volume.ID) (catalogEntry, error) {
	active, err := tapecatalog.ListActive(stateRoot)
	if err != nil {
		return catalogEntry{}, err
	}
	if !slices.Contains(active, id) {
		return catalogEntry{}, fmt.Errorf("active volume %s does not exist", id)
	}
	dir, err := tapecatalog.VolumeDir(stateRoot, id)
	if err != nil {
		return catalogEntry{}, err
	}
	header, err := tapevolume.Read(dir)
	if err != nil {
		return catalogEntry{}, err
	}
	observed, err := tapecatalog.LoadObserved(stateRoot, id)
	if err != nil {
		return catalogEntry{}, err
	}
	entries, err := os.ReadDir(tapevolume.SnapshotsDir(dir))
	if err != nil {
		return catalogEntry{}, fmt.Errorf("read catalog snapshots: %w", err)
	}
	snapshots := make(map[string]struct{})
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		snapshotDir := filepath.Join(tapevolume.SnapshotsDir(dir), entry.Name())
		manifest, readErr := tapemanifest.Read(snapshotDir)
		if errors.Is(readErr, tapemanifest.ErrIncomplete) {
			continue
		}
		if readErr != nil {
			return catalogEntry{}, fmt.Errorf(
				"read catalog snapshot %q: %w",
				entry.Name(),
				readErr,
			)
		}
		name, err := tapevolume.SnapshotName(
			manifest.MetadataRef,
			manifest.Generation,
		)
		if err != nil {
			return catalogEntry{}, err
		}
		if name != entry.Name() {
			return catalogEntry{}, fmt.Errorf(
				"catalog snapshot directory %q contains identity %q",
				entry.Name(),
				name,
			)
		}
		snapshots[name] = struct{}{}
	}
	return catalogEntry{
		volume:    header,
		observed:  observed,
		snapshots: snapshots,
	}, nil
}

func libraryMetadataRefs(libraryRoot string) (map[string]struct{}, error) {
	entries, err := os.ReadDir(libraryRoot)
	if err != nil {
		return nil, err
	}
	refs := make(map[string]struct{})
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(libraryRoot, entry.Name(), ".kura", "series.json")
		metadata, err := seriesmeta.Read(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read series %q: %w", entry.Name(), err)
		}
		refs[metadata.MetadataRef] = struct{}{}
	}
	return refs, nil
}

func admitPlans(stateRoot string, plans []backupplan.Plan) ([]admittedPlan, error) {
	admitted := make([]admittedPlan, 0, len(plans))
	for _, plan := range plans {
		ready, err := backupplan.ReadReady(stateRoot, plan.PlanID)
		if err != nil {
			return nil, fmt.Errorf(
				"executor: read ready plan %s: %w",
				plan.PlanID,
				err,
			)
		}
		if !reflect.DeepEqual(ready, plan) {
			return nil, fmt.Errorf(
				"executor: ready plan %s differs from supplied plan",
				plan.PlanID,
			)
		}
		pin, err := admitFillPlan(plan)
		if err != nil {
			return nil, err
		}
		admitted = append(admitted, admittedPlan{plan: plan, pin: pin})
	}
	return admitted, nil
}

func admitFillPlan(plan backupplan.Plan) (volume.ID, error) {
	var pin volume.ID
	for _, action := range plan.Actions {
		switch action.Type {
		case backupplan.ActionReformat, backupplan.ActionAdmit:
			return "", fmt.Errorf(
				"executor: plan %s contains init action %q; init sessions are not implemented",
				plan.PlanID,
				action.Type,
			)
		case backupplan.ActionImport, backupplan.ActionVerify:
			return "", fmt.Errorf(
				"executor: plan %s contains deferred action %q",
				plan.PlanID,
				action.Type,
			)
		case backupplan.ActionAssertVolume:
			if pin == "" {
				pin = action.VolumeID
			}
		case backupplan.ActionBackup:
			if pin == "" {
				return "", fmt.Errorf(
					"executor: plan %s must assert volume before backup",
					plan.PlanID,
				)
			}
		case backupplan.ActionAssertInventory, backupplan.ActionAssertFreeSpace:
		default:
			return "", fmt.Errorf(
				"executor: plan %s contains unknown action %q",
				plan.PlanID,
				action.Type,
			)
		}
	}
	if pin == "" {
		return "", fmt.Errorf(
			"executor: plan %s must assert volume before load-time sweep",
			plan.PlanID,
		)
	}
	return pin, nil
}

func leadingInventory(plan backupplan.Plan) (map[string]struct{}, error) {
	for _, action := range plan.Actions {
		if action.Type == backupplan.ActionBackup {
			break
		}
		if action.Type != backupplan.ActionAssertInventory {
			continue
		}
		expected := make(map[string]struct{}, len(action.Snapshots))
		for _, name := range action.Snapshots {
			expected[name] = struct{}{}
		}
		return expected, nil
	}
	return nil, fmt.Errorf(
		"executor: plan %s must assert inventory before backup",
		plan.PlanID,
	)
}

func retirePlan(
	stateRoot string,
	journal *backupplan.Writer,
	plan backupplan.Plan,
	cause error,
) error {
	logErr := appendSessionEvent(journal, backupplan.Event{
		Type:   backupplan.EventPlanFailed,
		PlanID: plan.PlanID,
		Reason: "halted",
		Detail: cause.Error(),
	})
	discardErr := backupplan.Discard(stateRoot, plan.PlanID)
	if discardErr != nil {
		discardErr = fmt.Errorf("executor: discard halted plan %s: %w", plan.PlanID, discardErr)
	}
	return errors.Join(logErr, discardErr)
}

func retirePendingPlans(
	stateRoot string,
	journal *backupplan.Writer,
	pending []admittedPlan,
	cause error,
) error {
	retirementErrors := make([]error, 0, len(pending))
	for _, admitted := range pending {
		if err := retirePlan(
			stateRoot,
			journal,
			admitted.plan,
			cause,
		); err != nil {
			retirementErrors = append(retirementErrors, err)
		}
	}
	return errors.Join(retirementErrors...)
}

func appendTerminal(
	journal *backupplan.Writer,
	reason string,
	plansCompleted int,
) error {
	return appendSessionEvent(journal, backupplan.Event{
		Type:           backupplan.EventTerminal,
		State:          "ended",
		Reason:         reason,
		PlansCompleted: plansCompleted,
	})
}

func appendSessionEvent(
	journal *backupplan.Writer,
	event backupplan.Event,
) error {
	event.Seq = journal.HighestSeq() + 1
	event.At = time.Now().UTC().Truncate(time.Second)
	if err := journal.Append(event); err != nil {
		return fmt.Errorf("executor: append session event: %w", err)
	}
	return nil
}

func sortedSet(set map[string]struct{}) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	slices.Sort(values)
	return values
}

func candidateTapeIDs(plans []admittedPlan) []tape.ID {
	candidates := make([]tape.ID, 0, len(plans))
	for _, plan := range plans {
		candidates = append(candidates, plan.plan.Target.TapeID)
	}
	return candidates
}

func matchingPlan(plans []admittedPlan, tapeID tape.ID) int {
	for index, plan := range plans {
		if plan.plan.Target.TapeID == tapeID {
			return index
		}
	}
	return -1
}

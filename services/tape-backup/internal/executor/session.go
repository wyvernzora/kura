package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/encryptionkey"
	"github.com/wyvernzora/kura/services/tape-backup/internal/planner"
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
	// ErrEncryptionKeySetupFailed reports that the drive rejected the
	// configured key before a write gate.
	ErrEncryptionKeySetupFailed = errors.New("executor: encryption_key_setup_failed")
)

var sessionRegistry struct {
	sync.Mutex
	active bool
}

// PreparedPlan is one plan whose loaded cartridge and recorded pre-state have
// been validated. DebrisSnapshots is forensic session state only.
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
	actionOffset       int
}

// SessionOptions controls the polling loop and per-snapshot commit policy.
type SessionOptions struct {
	PollInterval    time.Duration
	IdleTimeout     time.Duration
	StateRoot       string
	LibraryRoot     string
	FreeSpaceMargin int64
	FlushCadence    int
	LTFSRoot        string
	ProcMountsPath  string
	EncryptionKey   encryptionkey.Key
}

type admittedPlan struct {
	plan backupplan.Plan
	pin  volume.ID
	kind planKind
}

type planKind uint8

const (
	planKindFill planKind = iota + 1
	planKindInit
)

// RunSession executes ready fill and init plans synchronously as cartridges
// are swapped.
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
	availability, err := ClassifyDriveAvailability(drive, DriveAvailabilityOptions{
		LTFSRoot:       options.LTFSRoot,
		ProcMountsPath: options.ProcMountsPath,
	})
	if err != nil {
		return err
	}
	if availability.Reason != DriveReady &&
		availability.Reason != DriveWaitingForTape {
		return fmt.Errorf(
			"executor: %s: %s",
			availability.Reason,
			availability.Message,
		)
	}
	if !availability.CloseRequired {
		return errors.New("executor: drive custody was not acquired")
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
		prepared, err := prepareCartridge(
			ctx,
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
		if unmountErr := drive.Unmount(); unmountErr != nil {
			slog.Warn(
				"kura tape session unmount failed",
				"plan_id", selected.plan.PlanID,
				"error", unmountErr,
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

func prepareCartridge(
	ctx context.Context,
	drive Drive,
	identity LoadedIdentity,
	admitted admittedPlan,
	journal *backupplan.Writer,
	options SessionOptions,
) (PreparedPlan, error) {
	if admitted.kind == planKindInit {
		return prepareInitCartridge(ctx, drive, identity, admitted, journal, options)
	}
	return prepareFillCartridge(drive, identity, admitted, journal, options)
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
				return index, identity, nil
			}
		case identity.State == LoadedIdentityUnidentified:
			if index := matchingInitPlan(plans, identity.MediumSerial); index >= 0 {
				return index, identity, nil
			}
		case identity.State == LoadedIdentityNone:
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
	if err := setAndVerifyEncryption(
		drive,
		options.EncryptionKey,
		journal,
		plan.PlanID,
	); err != nil {
		return PreparedPlan{}, err
	}
	if err := drive.Mount(); err != nil {
		return PreparedPlan{}, fmt.Errorf(
			"executor: mount tape %s: %w",
			plan.Target.TapeID,
			err,
		)
	}
	identity, err := drive.LoadedIdentity()
	if err != nil {
		return PreparedPlan{}, fmt.Errorf(
			"executor: re-read mounted tape identity: %w",
			err,
		)
	}
	if err := validateKeyFingerprint(identity, options.EncryptionKey); err != nil {
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

func prepareInitCartridge(
	ctx context.Context,
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
	if err := validateInitPreFormat(
		drive,
		identity,
		admitted,
		journal,
		options,
	); err != nil {
		return PreparedPlan{}, err
	}
	if err := formatAndStampInitMedia(
		drive,
		identity,
		admitted,
		journal,
		options,
	); err != nil {
		return PreparedPlan{}, err
	}
	stamped, entry, err := installInitializedVolume(
		drive,
		identity.Cartridge.Root,
		admitted,
		options.StateRoot,
	)
	if err != nil {
		return PreparedPlan{}, err
	}
	if err := validateInitFillCapacity(
		drive,
		plan,
		options.FreeSpaceMargin,
	); err != nil {
		return PreparedPlan{}, err
	}
	if err := appendSessionEvent(journal, backupplan.Event{
		Type:     backupplan.EventTapeLoaded,
		TapeID:   stamped.TapeID,
		VolumeID: stamped.VolumeID,
	}); err != nil {
		return PreparedPlan{}, err
	}

	return PreparedPlan{
		Drive:              drive,
		Journal:            journal,
		Cartridge:          Cartridge{TapeID: plan.Target.TapeID, Root: identity.Cartridge.Root},
		Volume:             stamped,
		Plan:               plan,
		StateRoot:          options.StateRoot,
		LibraryRoot:        options.LibraryRoot,
		CatalogSnapshots:   entry.snapshots,
		DebrisSnapshots:    make(map[string]struct{}),
		CatalogObservation: entry.observed,
		actionOffset:       2,
	}, nil
}

func validateInitPreFormat(
	drive Drive,
	identity LoadedIdentity,
	admitted admittedPlan,
	journal *backupplan.Writer,
	options SessionOptions,
) error {
	plan := admitted.plan
	if err := setAndVerifyEncryption(
		drive,
		options.EncryptionKey,
		journal,
		plan.PlanID,
	); err != nil {
		return err
	}
	identity, err := drive.LoadedIdentity()
	if err != nil {
		return fmt.Errorf("executor: re-read init medium identity: %w", err)
	}
	if identity.MediumSerial != plan.Target.MediumSerial {
		return fmt.Errorf(
			"executor: plan %s pre-state mismatch: loaded medium serial %q, want %q",
			plan.PlanID,
			identity.MediumSerial,
			plan.Target.MediumSerial,
		)
	}
	if err := validateInitIdentity(identity, admitted, options.StateRoot); err != nil {
		return err
	}
	if err := validateKeyFingerprint(identity, options.EncryptionKey); err != nil {
		return err
	}
	return preflightInitBackups(
		options.LibraryRoot,
		plan,
		options.FreeSpaceMargin,
	)
}

func formatAndStampInitMedia(
	drive Drive,
	identity LoadedIdentity,
	admitted admittedPlan,
	journal *backupplan.Writer,
	options SessionOptions,
) error {
	plan := admitted.plan
	if identity.State == LoadedIdentityIdentified {
		if err := drive.Unmount(); err != nil {
			return fmt.Errorf(
				"executor: unmount tape %s before format: %w",
				plan.Target.TapeID,
				err,
			)
		}
	}
	if err := drive.Format(plan.Target.TapeID); err != nil {
		return fmt.Errorf("executor: format tape %s: %w", plan.Target.TapeID, err)
	}
	if err := drive.Mount(); err != nil {
		return fmt.Errorf("executor: mount tape %s: %w", plan.Target.TapeID, err)
	}
	if err := verifyEncryption(drive, journal, plan.PlanID); err != nil {
		if !errors.Is(err, ErrEncryptionInactive) {
			return err
		}
		if err := drive.SetEncryptionKey(options.EncryptionKey); err != nil {
			return fmt.Errorf("%w: %v", ErrEncryptionKeySetupFailed, err)
		}
		if err := verifyEncryption(drive, journal, plan.PlanID); err != nil {
			return err
		}
	}
	if err := drive.StampIdentity(admitted.pin, plan.Target.TapeID); err != nil {
		return fmt.Errorf(
			"executor: stamp tape %s identity: %w",
			plan.Target.TapeID,
			err,
		)
	}
	return nil
}

func validateInitFillCapacity(
	drive Drive,
	plan backupplan.Plan,
	freeSpaceMargin int64,
) error {
	_, free, err := drive.Capacity()
	if err != nil {
		return fmt.Errorf("executor: revalidate initialized tape capacity: %w", err)
	}
	var needed int64
	for _, action := range plan.Actions[2 : len(plan.Actions)-1] {
		if action.Bytes > math.MaxInt64-needed {
			return errors.New("executor: init fill set byte total overflows int64")
		}
		needed += action.Bytes
	}
	available := free - freeSpaceMargin
	if available < 0 || needed > available {
		return fmt.Errorf(
			"executor: initialized tape has %d bytes free with %d-byte margin; fill set needs %d bytes",
			free,
			freeSpaceMargin,
			needed,
		)
	}
	return nil
}

func installInitializedVolume(
	drive Drive,
	cartridgeRoot string,
	admitted admittedPlan,
	stateRoot string,
) (tapevolume.Volume, catalogEntry, error) {
	plan := admitted.plan
	stamped, err := tapevolume.Read(cartridgeRoot)
	if err != nil {
		return tapevolume.Volume{}, catalogEntry{}, fmt.Errorf(
			"executor: read stamped tape %s identity: %w",
			plan.Target.TapeID,
			err,
		)
	}
	if stamped.VolumeID != admitted.pin || stamped.TapeID != plan.Target.TapeID {
		return tapevolume.Volume{}, catalogEntry{}, fmt.Errorf(
			"executor: plan %s stamped identity mismatch: got volume %s on tape %s, want volume %s on tape %s",
			plan.PlanID,
			stamped.VolumeID,
			stamped.TapeID,
			admitted.pin,
			plan.Target.TapeID,
		)
	}
	if err := drive.Sync(); err != nil {
		return tapevolume.Volume{}, catalogEntry{}, fmt.Errorf(
			"executor: sync stamped tape identity: %w",
			err,
		)
	}
	observed, header, err := readInitializedVolumeFacts(
		drive,
		cartridgeRoot,
		plan.Target.TapeID,
	)
	if err != nil {
		return tapevolume.Volume{}, catalogEntry{}, err
	}
	if err := tapecatalog.InstallVolume(
		stateRoot,
		admitted.pin,
		header,
		observed,
	); err != nil {
		return tapevolume.Volume{}, catalogEntry{}, fmt.Errorf(
			"executor: install initialized volume %s: %w",
			admitted.pin,
			err,
		)
	}
	entry, err := loadCatalogEntry(stateRoot, admitted.pin)
	if err != nil {
		return tapevolume.Volume{}, catalogEntry{}, fmt.Errorf(
			"executor: verify installed volume %s: %w",
			admitted.pin,
			err,
		)
	}
	if entry.volume.VolumeID != admitted.pin ||
		entry.volume.TapeID != plan.Target.TapeID {
		return tapevolume.Volume{}, catalogEntry{}, fmt.Errorf(
			"executor: plan %s installed identity mismatch: got volume %s on tape %s, want volume %s on tape %s",
			plan.PlanID,
			entry.volume.VolumeID,
			entry.volume.TapeID,
			admitted.pin,
			plan.Target.TapeID,
		)
	}
	return stamped, entry, nil
}

func readInitializedVolumeFacts(
	drive Drive,
	cartridgeRoot string,
	tapeID tape.ID,
) (tapecatalog.Observed, []byte, error) {
	total, free, err := drive.Capacity()
	if err != nil {
		return tapecatalog.Observed{}, nil, fmt.Errorf(
			"executor: observe initialized tape capacity: %w",
			err,
		)
	}
	header, err := os.ReadFile(tapevolume.VolumeFile(cartridgeRoot))
	if err != nil {
		return tapecatalog.Observed{}, nil, fmt.Errorf(
			"executor: read stamped tape header: %w",
			err,
		)
	}
	return tapecatalog.Observed{
		TapeID:        tapeID,
		ObservedAt:    time.Now().UTC().Truncate(time.Second),
		CapacityBytes: total,
		FreeBytes:     free,
	}, header, nil
}

func validateInitIdentity(
	identity LoadedIdentity,
	admitted admittedPlan,
	stateRoot string,
) error {
	plan := admitted.plan
	switch identity.State {
	case LoadedIdentityUnidentified:
		return nil
	case LoadedIdentityIdentified:
		if identity.Volume.TapeID != plan.Target.TapeID {
			return fmt.Errorf(
				"executor: plan %s pre-state mismatch: loaded volume %s is on tape %s, want tape %s",
				plan.PlanID,
				identity.Volume.VolumeID,
				identity.Volume.TapeID,
				plan.Target.TapeID,
			)
		}
	default:
		return fmt.Errorf(
			"executor: plan %s pre-state mismatch: loaded identity state is %q, want unidentified or identified healthy-empty media",
			plan.PlanID,
			identity.State,
		)
	}

	observedVolumeID := identity.Volume.VolumeID
	active, err := tapecatalog.ListActive(stateRoot)
	if err != nil {
		return fmt.Errorf(
			"executor: plan %s inspect init catalog recognition: %w",
			plan.PlanID,
			err,
		)
	}
	recognized := slices.Contains(active, observedVolumeID)
	reason := "adoption_deferred"
	if recognized {
		reason = "divergence_deferred"
		entry, err := loadCatalogEntry(stateRoot, observedVolumeID)
		if err != nil {
			return fmt.Errorf(
				"executor: plan %s %s: recognized volume %s catalog entry is not complete: %w",
				plan.PlanID,
				reason,
				observedVolumeID,
				err,
			)
		}
		if entry.volume.VolumeID != observedVolumeID ||
			entry.volume.TapeID != plan.Target.TapeID {
			return fmt.Errorf(
				"executor: plan %s %s: loaded volume %s on tape %s does not match catalog volume %s on tape %s",
				plan.PlanID,
				reason,
				observedVolumeID,
				plan.Target.TapeID,
				entry.volume.VolumeID,
				entry.volume.TapeID,
			)
		}
		if len(entry.snapshots) != 0 {
			return fmt.Errorf(
				"executor: plan %s %s: recognized volume %s catalog records %d committed snapshots",
				plan.PlanID,
				reason,
				observedVolumeID,
				len(entry.snapshots),
			)
		}
	}

	committed, err := inspectInitWitness(identity.Cartridge.Root)
	if err != nil {
		return fmt.Errorf(
			"executor: plan %s %s: volume %s healthy-empty witness: %w",
			plan.PlanID,
			reason,
			observedVolumeID,
			err,
		)
	}
	if committed != 0 {
		return fmt.Errorf(
			"executor: plan %s %s: volume %s has %d committed snapshots",
			plan.PlanID,
			reason,
			observedVolumeID,
			committed,
		)
	}
	return nil
}

func inspectInitWitness(root string) (int, error) {
	snapshotsDir := tapevolume.SnapshotsDir(root)
	info, err := os.Lstat(snapshotsDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, errors.New("snapshots namespace is absent")
	}
	if err != nil {
		return 0, fmt.Errorf("inspect snapshots namespace: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, errors.New("snapshots namespace is not a real directory")
	}
	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		return 0, fmt.Errorf("enumerate snapshots namespace: %w", err)
	}
	committed := 0
	for _, entry := range entries {
		marker, err := os.Lstat(filepath.Join(snapshotsDir, entry.Name(), "complete.json"))
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			return 0, fmt.Errorf(
				"inspect snapshot %q completion marker: %w",
				entry.Name(),
				err,
			)
		case marker.Mode().IsRegular() && marker.Mode()&os.ModeSymlink == 0:
			committed++
		}
	}
	return committed, nil
}

func preflightInitBackups(
	libraryRoot string,
	plan backupplan.Plan,
	freeSpaceMargin int64,
) error {
	var totalBytes int64
	for _, action := range plan.Actions[2 : len(plan.Actions)-1] {
		if reason, detail := preflightBackup(libraryRoot, action); reason != "" {
			return fmt.Errorf(
				"executor: init backup action (%q, %d) failed preflight: %s: %s",
				action.MetadataRef,
				action.Generation,
				reason,
				detail,
			)
		}
		if action.Bytes > math.MaxInt64-totalBytes {
			return errors.New("executor: init fill set byte total overflows int64")
		}
		totalBytes += action.Bytes
	}
	nominalCapacity, err := planner.NominalCapacity(plan.Target.TapeID)
	if err != nil {
		return fmt.Errorf("executor: inspect init nominal capacity: %w", err)
	}
	available := nominalCapacity - freeSpaceMargin
	if available < 0 || totalBytes > available {
		return fmt.Errorf(
			"executor: init fill set needs %d bytes; %d-byte margin, %d bytes nominal capacity",
			totalBytes,
			freeSpaceMargin,
			nominalCapacity,
		)
	}
	return nil
}

func verifyEncryption(
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

func setAndVerifyEncryption(
	drive Drive,
	key encryptionkey.Key,
	journal *backupplan.Writer,
	planID string,
) error {
	if err := drive.SetEncryptionKey(key); err != nil {
		return fmt.Errorf("%w: %v", ErrEncryptionKeySetupFailed, err)
	}
	return verifyEncryption(drive, journal, planID)
}

func validateKeyFingerprint(identity LoadedIdentity, key encryptionkey.Key) error {
	if identity.State != LoadedIdentityIdentified {
		return nil
	}
	current := key.Fingerprint()
	written := identity.Volume.KeyFingerprint
	if written == current {
		return nil
	}
	return fmt.Errorf(
		"executor: encryption_key_mismatch: written under key %s, current key is %s",
		written,
		current,
	)
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
		admittedPlan, err := admitPlan(plan)
		if err != nil {
			return nil, err
		}
		admitted = append(admitted, admittedPlan)
	}
	return admitted, nil
}

func admitPlan(plan backupplan.Plan) (admittedPlan, error) {
	for _, action := range plan.Actions {
		if action.Type == backupplan.ActionImport ||
			action.Type == backupplan.ActionVerify {
			return admittedPlan{}, fmt.Errorf(
				"executor: plan %s contains deferred action %q",
				plan.PlanID,
				action.Type,
			)
		}
	}
	if planContainsInitAction(plan) {
		pin, err := admitInitPlan(plan)
		if err != nil {
			return admittedPlan{}, err
		}
		return admittedPlan{
			plan: plan,
			pin:  pin,
			kind: planKindInit,
		}, nil
	}

	pin, err := admitFillPlan(plan)
	if err != nil {
		return admittedPlan{}, err
	}
	return admittedPlan{
		plan: plan,
		pin:  pin,
		kind: planKindFill,
	}, nil
}

func planContainsInitAction(plan backupplan.Plan) bool {
	for _, action := range plan.Actions {
		if action.Type == backupplan.ActionReformat ||
			action.Type == backupplan.ActionAdmit {
			return true
		}
	}
	return false
}

func admitInitPlan(plan backupplan.Plan) (volume.ID, error) {
	if plan.Target.MediumSerial == "" {
		return "", fmt.Errorf(
			"executor: plan %s init target medium serial is required",
			plan.PlanID,
		)
	}
	if len(plan.Actions) < 3 ||
		plan.Actions[0].Type != backupplan.ActionReformat ||
		plan.Actions[1].Type != backupplan.ActionAdmit ||
		plan.Actions[len(plan.Actions)-1].Type != backupplan.ActionAssertInventory {
		return "", fmt.Errorf(
			"executor: plan %s reformat/admit actions do not match the init plan shape",
			plan.PlanID,
		)
	}

	expected := make([]string, 0, len(plan.Actions)-3)
	for _, action := range plan.Actions[2 : len(plan.Actions)-1] {
		if action.Type != backupplan.ActionBackup {
			return "", fmt.Errorf(
				"executor: plan %s reformat/admit actions do not match the init plan shape",
				plan.PlanID,
			)
		}
		name, err := tapevolume.SnapshotName(action.MetadataRef, action.Generation)
		if err != nil {
			return "", fmt.Errorf(
				"executor: plan %s name init backup target: %w",
				plan.PlanID,
				err,
			)
		}
		expected = append(expected, name)
	}
	slices.Sort(expected)
	trailing := slices.Clone(plan.Actions[len(plan.Actions)-1].Snapshots)
	slices.Sort(trailing)
	if !slices.Equal(expected, trailing) {
		return "", fmt.Errorf(
			"executor: plan %s trailing init inventory %v does not match backups %v",
			plan.PlanID,
			trailing,
			expected,
		)
	}
	return plan.Actions[1].VolumeID, nil
}

func admitFillPlan(plan backupplan.Plan) (volume.ID, error) {
	var pin volume.ID
	for _, action := range plan.Actions {
		switch action.Type {
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

func matchingInitPlan(plans []admittedPlan, mediumSerial string) int {
	onlyInit := -1
	for index, plan := range plans {
		if plan.kind != planKindInit {
			continue
		}
		if onlyInit == -1 {
			onlyInit = index
		} else {
			onlyInit = -2
		}
		if plan.plan.Target.MediumSerial == mediumSerial {
			return index
		}
	}
	if onlyInit >= 0 {
		return onlyInit
	}
	return -1
}

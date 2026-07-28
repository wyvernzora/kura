package executor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
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
	errCartridgeRejected  = errors.New("executor: cartridge rejected")
)

// EventSink accepts a best-effort copy of a durable executor event.
// Implementations must honor ctx.
type EventSink interface {
	Deliver(ctx context.Context, event backupplan.Event) error
}

// OutboxOptions controls asynchronous best-effort event delivery.
type OutboxOptions struct {
	Capacity    int
	CallTimeout time.Duration
}

// EventOutbox writes sequenced events to a session log before offering them to
// one bounded, best-effort delivery goroutine.
type EventOutbox struct {
	mu         sync.Mutex
	journal    *backupplan.Writer
	sink       EventSink
	options    OutboxOptions
	queue      []backupplan.Event
	nextSeq    uint64
	overflowed bool
	closed     bool
	wake       chan struct{}
	done       chan struct{}
}

// NewEventOutbox starts one best-effort delivery goroutine. The caller retains
// ownership of journal and must close it after the outbox is closed. For a
// resumed journal, re-offer its recorded events in order before emitting new
// events.
func NewEventOutbox(
	journal *backupplan.Writer,
	sink EventSink,
	options OutboxOptions,
) (*EventOutbox, error) {
	if journal == nil {
		return nil, errors.New("executor: session journal is required")
	}
	if sink == nil {
		return nil, errors.New("executor: event sink is required")
	}
	if options.Capacity < 1 {
		return nil, errors.New("executor: outbox capacity must be at least 1")
	}
	if options.CallTimeout <= 0 {
		return nil, errors.New("executor: outbox call timeout must be greater than zero")
	}
	outbox := &EventOutbox{
		journal: journal,
		sink:    sink,
		options: options,
		queue:   make([]backupplan.Event, 0, options.Capacity),
		nextSeq: journal.HighestSeq(),
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	go outbox.deliver()
	return outbox, nil
}

// Emit assigns the event's timestamp and per-session sequence, synchronously
// records it, then offers it to the bounded relay without waiting for the sink.
func (o *EventOutbox) Emit(event backupplan.Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return errors.New("executor: emit to closed event outbox")
	}

	if len(o.queue) >= o.options.Capacity && !o.overflowed {
		overflow, err := o.record(backupplan.Event{Type: backupplan.EventOutboxOverflow})
		if err != nil {
			return err
		}
		o.overflowed = true
		o.queue[len(o.queue)-1] = overflow
	}
	event, err := o.record(event)
	if err != nil {
		return err
	}
	o.offer(event)
	return nil
}

// Reoffer offers a previously recorded event to the relay without changing its
// sequence or timestamp.
func (o *EventOutbox) Reoffer(event backupplan.Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return errors.New("executor: re-offer to closed event outbox")
	}
	if !o.offer(event) {
		return errors.New("executor: event relay is full")
	}
	return nil
}

func (o *EventOutbox) offer(event backupplan.Event) bool {
	if len(o.queue) < o.options.Capacity {
		o.queue = append(o.queue, event)
		o.notify()
		return true
	}
	return false
}

// Close stops accepting events and waits for all queued relay attempts to
// finish. Failed attempts are dropped; if ctx expires, delivery continues and
// Close may be called again.
func (o *EventOutbox) Close(ctx context.Context) error {
	o.mu.Lock()
	o.closed = true
	o.notify()
	o.mu.Unlock()
	select {
	case <-o.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("executor: close event outbox: %w", ctx.Err())
	}
}

func (o *EventOutbox) record(event backupplan.Event) (backupplan.Event, error) {
	event.Seq = o.nextSeq + 1
	event.At = time.Now().UTC().Truncate(time.Second)
	if err := o.journal.Append(event); err != nil {
		o.nextSeq = o.journal.HighestSeq()
		return backupplan.Event{}, fmt.Errorf("executor: append session event: %w", err)
	}
	o.nextSeq = event.Seq
	return event, nil
}

func (o *EventOutbox) notify() {
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *EventOutbox) deliver() {
	defer close(o.done)
	for {
		event, ok := o.front()
		if !ok {
			return
		}
		if event.Type == "" {
			<-o.wake
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), o.options.CallTimeout)
		err := o.sink.Deliver(ctx, event)
		cancel()
		if err != nil {
			continue
		}
	}
}

func (o *EventOutbox) front() (backupplan.Event, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.queue) > 0 {
		event := o.queue[0]
		o.queue[0] = backupplan.Event{}
		o.queue = o.queue[1:]
		if len(o.queue) == 0 {
			o.queue = make([]backupplan.Event, 0, o.options.Capacity)
			o.overflowed = false
		}
		return event, true
	}
	if o.closed {
		return backupplan.Event{}, false
	}
	return backupplan.Event{}, true
}

// PreparedPlan is the hand-off point to the next slice's phase 2 and phase 3.
// The cartridge is mounted, encryption is active, identity is confirmed, and
// load-time normalization has completed.
type PreparedPlan struct {
	Drive              Drive
	Events             *EventOutbox
	Cartridge          Cartridge
	Volume             tapevolume.Volume
	Plan               backupplan.Plan
	LibraryRoot        string
	CommittedSnapshots map[string]struct{}
}

// PreparedPlanHandler owns phase 2 classification and phase 3 execution.
type PreparedPlanHandler func(context.Context, PreparedPlan) error

// SessionOptions controls the polling session loop.
type SessionOptions struct {
	PollInterval time.Duration
	IdleTimeout  time.Duration
	LibraryRoot  string
}

type admittedPlan struct {
	plan backupplan.Plan
	pin  volume.ID
}

// RunSession works through the supplied ready plans as cartridges are swapped.
// It stops at PreparedPlanHandler; no backup action is implemented here.
func RunSession(
	ctx context.Context,
	drive Drive,
	plans []backupplan.Plan,
	events *EventOutbox,
	handle PreparedPlanHandler,
	options SessionOptions,
) error {
	if err := validateRunSession(drive, events, handle, options); err != nil {
		return err
	}
	pending, err := admitPlans(plans)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return ErrNoReadyPlans
	}

	plansAdmitted := len(pending)
	plansCompleted := 0
	idleDeadline := time.Now().Add(options.IdleTimeout)
	for len(pending) > 0 {
		if err := events.Emit(backupplan.Event{
			Type:       backupplan.EventWaitingForTape,
			Candidates: candidateTapeIDs(pending),
		}); err != nil {
			return err
		}

		index, cartridge, err := waitForCandidate(
			ctx,
			drive,
			pending,
			options.PollInterval,
			idleDeadline,
		)
		if errors.Is(err, errIdleTimeout) {
			if emitErr := events.Emit(backupplan.Event{
				Type:           backupplan.EventTerminal,
				State:          "ended",
				Reason:         "idle_timeout",
				PlansCompleted: plansCompleted,
			}); emitErr != nil {
				return emitErr
			}
			return nil
		}
		if err != nil {
			return err
		}

		prepared, err := prepareCartridge(
			drive,
			cartridge,
			pending[index],
			events,
			options.LibraryRoot,
		)
		if errors.Is(err, errCartridgeRejected) {
			if emitErr := events.Emit(backupplan.Event{
				Type:   backupplan.EventPlanFailed,
				PlanID: pending[index].plan.PlanID,
				Reason: "cartridge_rejected",
				Detail: err.Error(),
			}); emitErr != nil {
				return errors.Join(err, emitErr)
			}
			pending = append(pending[:index], pending[index+1:]...)
			idleDeadline = time.Now().Add(options.IdleTimeout)
			continue
		}
		if err != nil {
			return err
		}
		if err := handle(ctx, prepared); errors.Is(err, ErrPlanFailed) {
			pending = append(pending[:index], pending[index+1:]...)
			idleDeadline = time.Now().Add(options.IdleTimeout)
			continue
		} else if err != nil {
			return fmt.Errorf("executor: handle prepared plan %s: %w", prepared.Plan.PlanID, err)
		}
		pending = append(pending[:index], pending[index+1:]...)
		plansCompleted++
		idleDeadline = time.Now().Add(options.IdleTimeout)
	}
	terminalReason := "completed"
	if plansCompleted < plansAdmitted {
		terminalReason = "plans_failed"
	}
	return events.Emit(backupplan.Event{
		Type:           backupplan.EventTerminal,
		State:          "ended",
		Reason:         terminalReason,
		PlansCompleted: plansCompleted,
	})
}

func validateRunSession(
	drive Drive,
	events *EventOutbox,
	handle PreparedPlanHandler,
	options SessionOptions,
) error {
	if drive == nil {
		return errors.New("executor: drive is required")
	}
	if events == nil {
		return errors.New("executor: event outbox is required")
	}
	if handle == nil {
		return errors.New("executor: prepared plan handler is required")
	}
	if options.PollInterval <= 0 {
		return errors.New("executor: poll interval must be greater than zero")
	}
	if options.IdleTimeout <= 0 {
		return errors.New("executor: idle timeout must be greater than zero")
	}
	if options.LibraryRoot == "" {
		return errors.New("executor: library root is required")
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
) (int, Cartridge, error) {
	for {
		cartridge, err := drive.Loaded()
		switch {
		case err == nil:
			if index := matchingPlan(plans, cartridge.TapeID); index >= 0 {
				if _, _, capacityErr := drive.Capacity(); capacityErr == nil {
					return index, cartridge, nil
				} else if !errors.Is(capacityErr, ErrNotMounted) {
					return -1, Cartridge{}, fmt.Errorf(
						"executor: inspect mounted cartridge capacity: %w",
						capacityErr,
					)
				}
			}
		case errors.Is(err, ErrNoCartridge):
		default:
			return -1, Cartridge{}, fmt.Errorf("executor: identify loaded cartridge: %w", err)
		}

		remaining := time.Until(idleDeadline)
		if remaining <= 0 {
			return -1, Cartridge{}, errIdleTimeout
		}
		delay := min(pollInterval, remaining)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return -1, Cartridge{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func prepareCartridge(
	drive Drive,
	cartridge Cartridge,
	plan admittedPlan,
	events *EventOutbox,
	libraryRoot string,
) (PreparedPlan, error) {
	if err := events.Emit(backupplan.Event{
		Type:   backupplan.EventPlanStarted,
		PlanID: plan.plan.PlanID,
	}); err != nil {
		return PreparedPlan{}, err
	}

	encryptionActive, err := drive.EncryptionActive()
	if err != nil {
		return PreparedPlan{}, fmt.Errorf("executor: check drive encryption: %w", err)
	}
	if !encryptionActive {
		if emitErr := events.Emit(backupplan.Event{
			Type:   backupplan.EventTapeRejected,
			TapeID: cartridge.TapeID,
			Reason: "encryption_inactive",
		}); emitErr != nil {
			return PreparedPlan{}, errors.Join(ErrEncryptionInactive, emitErr)
		}
		return PreparedPlan{}, errors.Join(errCartridgeRejected, ErrEncryptionInactive)
	}
	if err := events.Emit(backupplan.Event{
		Type:   backupplan.EventEncryptionVerified,
		PlanID: plan.plan.PlanID,
	}); err != nil {
		return PreparedPlan{}, err
	}

	loadedVolume, err := tapevolume.Read(cartridge.Root)
	if err != nil {
		identityErr := fmt.Errorf(
			"executor: confirm cartridge %s identity: %w",
			cartridge.TapeID,
			err,
		)
		if emitErr := events.Emit(backupplan.Event{
			Type:   backupplan.EventTapeRejected,
			TapeID: cartridge.TapeID,
			Reason: "identity_unconfirmed",
			Detail: err.Error(),
		}); emitErr != nil {
			return PreparedPlan{}, errors.Join(identityErr, emitErr)
		}
		return PreparedPlan{}, errors.Join(errCartridgeRejected, identityErr)
	}
	if loadedVolume.VolumeID != plan.pin {
		identityErr := fmt.Errorf(
			"executor: cartridge %s volume is %s, plan %s requires %s",
			cartridge.TapeID,
			loadedVolume.VolumeID,
			plan.plan.PlanID,
			plan.pin,
		)
		if emitErr := events.Emit(backupplan.Event{
			Type:   backupplan.EventTapeRejected,
			TapeID: cartridge.TapeID,
			Reason: "volume_mismatch",
			Detail: fmt.Sprintf("loaded %s, expected %s", loadedVolume.VolumeID, plan.pin),
		}); emitErr != nil {
			return PreparedPlan{}, errors.Join(identityErr, emitErr)
		}
		return PreparedPlan{}, errors.Join(errCartridgeRejected, identityErr)
	}
	if err := events.Emit(backupplan.Event{
		Type:     backupplan.EventTapeLoaded,
		TapeID:   cartridge.TapeID,
		VolumeID: loadedVolume.VolumeID,
	}); err != nil {
		return PreparedPlan{}, err
	}
	committed, err := sweepSnapshots(cartridge.Root, events)
	if err != nil {
		if emitErr := events.Emit(backupplan.Event{
			Type:   backupplan.EventTapeRejected,
			TapeID: cartridge.TapeID,
			Reason: "sweep_failed",
			Detail: err.Error(),
		}); emitErr != nil {
			return PreparedPlan{}, errors.Join(errCartridgeRejected, err, emitErr)
		}
		return PreparedPlan{}, errors.Join(errCartridgeRejected, err)
	}
	return PreparedPlan{
		Drive:              drive,
		Events:             events,
		Cartridge:          cartridge,
		Volume:             loadedVolume,
		Plan:               plan.plan,
		LibraryRoot:        libraryRoot,
		CommittedSnapshots: committed,
	}, nil
}

func admitPlans(plans []backupplan.Plan) ([]admittedPlan, error) {
	admitted := make([]admittedPlan, 0, len(plans))
	for _, plan := range plans {
		pin, err := admitPlan(plan)
		if err != nil {
			return nil, err
		}
		admitted = append(admitted, admittedPlan{plan: plan, pin: pin})
	}
	return admitted, nil
}

func admitPlan(plan backupplan.Plan) (volume.ID, error) {
	var pin volume.ID
	for _, action := range plan.Actions {
		switch action.Type {
		case backupplan.ActionReformat,
			backupplan.ActionAdmit,
			backupplan.ActionImport,
			backupplan.ActionVerify:
			return "", fmt.Errorf(
				"executor: plan %s contains out-of-scope action %q",
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

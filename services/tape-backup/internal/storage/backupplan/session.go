package backupplan

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/wyvernzora/kura/services/tape-backup/internal/snapshotname"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

// EventType is the durable session-log event vocabulary.
type EventType string

const (
	EventWaitingForTape     EventType = "waiting_for_tape"
	EventTapeLoaded         EventType = "tape_loaded"
	EventTapeRejected       EventType = "tape_rejected"
	EventPlanStarted        EventType = "plan_started"
	EventPlanCompleted      EventType = "plan_completed"
	EventPlanFailed         EventType = "plan_failed"
	EventPlanCancelled      EventType = "plan_cancelled"
	EventEncryptionVerified EventType = "encryption_verified"
	EventSnapshotSwept      EventType = "snapshot_swept"
	EventOutboxOverflow     EventType = "outbox_overflow"
	EventDivergenceChecked  EventType = "divergence_checked"
	EventFreshnessChecked   EventType = "freshness_checked"
	EventItemStarted        EventType = "item_started"
	EventItemCompleted      EventType = "item_completed"
	EventItemFailed         EventType = "item_failed"
	EventItemSkipped        EventType = "item_skipped"
	EventFileStarted        EventType = "file_started"
	EventFileCompleted      EventType = "file_completed"
	EventCancelRequested    EventType = "cancel_requested"
	EventTerminal           EventType = "terminal"
)

const eventFileProgress EventType = "file_progress"

// ItemReason is the closed reason vocabulary for item failures, freshness
// drops, and item skips.
type ItemReason string

const (
	ReasonPayloadDrift        ItemReason = "payload_drift"
	ReasonClaimStale          ItemReason = "claim_stale"
	ReasonInsufficientSpace   ItemReason = "insufficient_space"
	ReasonChecksumMismatch    ItemReason = "checksum_mismatch"
	ReasonAlreadyPresent      ItemReason = "already_present"
	ReasonUnsupportedFileType ItemReason = "unsupported_file_type"
	ReasonSeriesMoved         ItemReason = "series_moved"
	ReasonSeriesRootMissing   ItemReason = "series_root_missing"
	ReasonStagedIntent        ItemReason = "staged_intent"
	ReasonWriteFailed         ItemReason = "write_failed"
)

// ErrSessionTerminal reports an append attempted after the terminal event.
var ErrSessionTerminal = errors.New("backupplan: append after terminal event")

// Header is the first record in a session log.
type Header struct {
	SessionID  string
	Executor   Creator
	StartedAt  time.Time
	ReadyPlans []string
}

// DroppedItem records an item removed by the freshness gate.
type DroppedItem struct {
	MetadataRef string
	Generation  int
	Reason      string
}

// Event is one durable session event. Fields not used by its Type are omitted.
type Event struct {
	Type           EventType
	At             time.Time
	Seq            uint64
	PlanID         string
	Candidates     []tape.ID
	TapeID         tape.ID
	VolumeID       volume.ID
	Result         string
	ExtraSnapshots []string
	Dropped        []DroppedItem
	MetadataRef    string
	Generation     int
	Bytes          int64
	Files          int
	Path           string
	Entry          string
	Snapshot       string
	ItemsWritten   int
	ItemsFailed    int
	Reason         string
	Detail         string
	State          string
	PlansCompleted int
}

// Session is the fully decoded view of one forensic log. Terminal is nil when
// execution was still in flight or the writer stopped before the closing line.
type Session struct {
	Header   Header
	Events   []Event
	Terminal *Event
}

type sessionHeaderWire struct {
	Type          string      `json:"type"`
	SchemaVersion int         `json:"schemaVersion"`
	SessionID     string      `json:"sessionId"`
	Executor      creatorWire `json:"executor"`
	StartedAt     string      `json:"startedAt"`
	ReadyPlans    []string    `json:"readyPlans"`
}

type droppedItemWire struct {
	MetadataRef string `json:"metadataRef"`
	Generation  int    `json:"generation"`
	Reason      string `json:"reason,omitempty"`
}

type eventWire struct {
	Type           EventType         `json:"type"`
	At             string            `json:"at"`
	Seq            uint64            `json:"seq"`
	PlanID         string            `json:"planId,omitempty"`
	Candidates     []string          `json:"candidates,omitempty"`
	TapeID         string            `json:"tapeId,omitempty"`
	VolumeID       string            `json:"volumeId,omitempty"`
	Result         string            `json:"result,omitempty"`
	ExtraSnapshots []string          `json:"extraSnapshots,omitempty"`
	Dropped        []droppedItemWire `json:"dropped,omitempty"`
	MetadataRef    string            `json:"metadataRef,omitempty"`
	Generation     int               `json:"generation,omitempty"`
	Bytes          int64             `json:"bytes,omitempty"`
	Files          int               `json:"files,omitempty"`
	Path           string            `json:"path,omitempty"`
	Entry          string            `json:"entry,omitempty"`
	Snapshot       string            `json:"snapshot,omitempty"`
	ItemsWritten   int               `json:"itemsWritten,omitempty"`
	ItemsFailed    int               `json:"itemsFailed,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	Detail         string            `json:"detail,omitempty"`
	State          string            `json:"state,omitempty"`
	PlansCompleted int               `json:"plansCompleted,omitempty"`
}

// Writer appends and fsyncs durable session events. Replays of a written but
// unsynced sequence retry the sync without writing another line.
type Writer struct {
	mu         sync.Mutex
	file       *os.File
	encoder    *json.Encoder
	sync       func() error
	highestSeq uint64
	syncedSeq  uint64
	terminal   bool
}

// CreateSession creates a new session log, writes and fsyncs its header, and
// returns an append writer.
func CreateSession(stateRoot string, header Header) (*Writer, error) {
	if err := validateHeader(header); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(sessionsDir(stateRoot), 0o775); err != nil {
		return nil, fmt.Errorf("backupplan: create sessions directory: %w", err)
	}
	path := sessionFile(stateRoot, header.SessionID)
	file, err := os.OpenFile(
		path,
		os.O_CREATE|os.O_EXCL|os.O_RDWR|os.O_APPEND,
		0o664,
	)
	if err != nil {
		return nil, fmt.Errorf("backupplan: create session %s: %w", header.SessionID, err)
	}
	if err := lockSessionFile(file); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("backupplan: lock session %s: %w", header.SessionID, err)
	}
	writer := newSessionWriter(file)
	wire := sessionHeaderWire{
		Type:          "header",
		SchemaVersion: schemaVersion,
		SessionID:     header.SessionID,
		Executor: creatorWire{
			Version: header.Executor.Version,
			Host:    header.Executor.Host,
		},
		StartedAt:  header.StartedAt.UTC().Format(time.RFC3339),
		ReadyPlans: copyStrings(header.ReadyPlans),
	}
	if err := writer.encoder.Encode(wire); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("backupplan: write session header: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("backupplan: sync session header: %w", err)
	}
	return writer, nil
}

// OpenSession exclusively locks and validates an existing session log for
// resumed appends. A second live writer receives an os.ErrExist-wrapping error
// until Close releases the lock. Terminal sessions are unlocked before return.
// The kernel also releases the lock if the process exits. A truncated final
// fragment is discarded before the next event is written.
func OpenSession(stateRoot, sessionID string) (*Writer, error) {
	if err := validateULID("sessionID", sessionID); err != nil {
		return nil, fmt.Errorf("backupplan: %w", err)
	}
	path := sessionFile(stateRoot, sessionID)
	file, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return nil, fmt.Errorf("backupplan: open session %s: %w", sessionID, err)
	}
	if err := lockSessionFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("backupplan: lock session %s: %w", sessionID, err)
	}
	session, err := readSessionRecords(bufio.NewReader(file), sessionID)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := dropTruncatedSessionTail(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("backupplan: resume session %s: %w", sessionID, err)
	}

	writer := newSessionWriter(file)
	for _, event := range session.Events {
		writer.highestSeq = max(writer.highestSeq, event.Seq)
	}
	if session.Terminal != nil {
		writer.highestSeq = max(writer.highestSeq, session.Terminal.Seq)
		writer.terminal = true
		if err := unlockSessionFile(file); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("backupplan: unlock terminal session %s: %w", sessionID, err)
		}
	}
	writer.syncedSeq = writer.highestSeq
	return writer, nil
}

// Append writes and fsyncs one durable event. Replayed sequence numbers append
// no second line; a replay retries any sync that previously failed.
func (w *Writer) Append(event Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := validateEvent(event); err != nil {
		return err
	}
	if w.file == nil {
		return errors.New("backupplan: append to closed session")
	}
	if event.Seq <= w.highestSeq {
		if event.Seq > w.syncedSeq {
			if err := w.sync(); err != nil {
				return fmt.Errorf("backupplan: sync session event: %w", err)
			}
			w.syncedSeq = w.highestSeq
		}
		return nil
	}
	if w.terminal {
		return ErrSessionTerminal
	}
	if err := w.encoder.Encode(toEventWire(event)); err != nil {
		return fmt.Errorf("backupplan: append session event: %w", err)
	}
	w.highestSeq = event.Seq
	if event.Type == EventTerminal {
		w.terminal = true
	}
	if err := w.sync(); err != nil {
		return fmt.Errorf("backupplan: sync session event: %w", err)
	}
	w.syncedSeq = w.highestSeq
	return nil
}

// HighestSeq reports the highest event sequence already recorded.
func (w *Writer) HighestSeq() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.highestSeq
}

// Close releases the session file. It is idempotent.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	closeErr := w.file.Close()
	w.file = nil
	return closeErr
}

func newSessionWriter(file *os.File) *Writer {
	return &Writer{
		file:    file,
		encoder: json.NewEncoder(file),
		sync:    file.Sync,
	}
}

func lockSessionFile(file *os.File) error {
	if err := syscall.Flock(
		int(file.Fd()),
		syscall.LOCK_EX|syscall.LOCK_NB,
	); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("session is already locked: %w", os.ErrExist)
		}
		return err
	}
	return nil
}

func unlockSessionFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

func dropTruncatedSessionTail(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect session log: %w", err)
	}
	size := info.Size()
	if size == 0 {
		return errors.New("session log has no complete records")
	}
	var last [1]byte
	if _, err := file.ReadAt(last[:], size-1); err != nil {
		return fmt.Errorf("inspect session log tail: %w", err)
	}
	if last[0] == '\n' {
		return nil
	}

	const chunkSize int64 = 4096
	for end := size; end > 0; {
		start := max(int64(0), end-chunkSize)
		chunk := make([]byte, end-start)
		n, readErr := file.ReadAt(chunk, start)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return fmt.Errorf("inspect session log tail: %w", readErr)
		}
		if index := bytes.LastIndexByte(chunk[:n], '\n'); index >= 0 {
			if err := file.Truncate(start + int64(index) + 1); err != nil {
				return fmt.Errorf("discard truncated session line: %w", err)
			}
			if err := file.Sync(); err != nil {
				return fmt.Errorf("sync truncated session log: %w", err)
			}
			return nil
		}
		end = start
	}
	return errors.New("session log has no complete records")
}

// ReadSession reads a session log. A missing terminal line is valid, and any
// final fragment without a trailing newline is ignored.
func ReadSession(stateRoot, sessionID string) (Session, error) {
	if err := validateULID("sessionID", sessionID); err != nil {
		return Session{}, fmt.Errorf("backupplan: %w", err)
	}
	path := sessionFile(stateRoot, sessionID)
	file, err := os.Open(path)
	if err != nil {
		return Session{}, fmt.Errorf("backupplan: read session %s: %w", sessionID, err)
	}
	defer file.Close()
	return readSessionRecords(bufio.NewReader(file), sessionID)
}

func readSessionRecords(reader *bufio.Reader, sessionID string) (Session, error) {
	var session Session
	headerSeen := false
	var previousSeq uint64
	for lineNumber := 1; ; lineNumber++ {
		line, truncated, err := nextSessionLine(reader, lineNumber)
		if err != nil {
			return session, err
		}
		if truncated {
			break
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if session.Terminal != nil {
			return session, fmt.Errorf(
				"backupplan: session line %d follows terminal seq %d",
				lineNumber,
				session.Terminal.Seq,
			)
		}
		if !utf8.Valid(line) {
			return session, fmt.Errorf(
				"backupplan: session line %d must be valid UTF-8",
				lineNumber,
			)
		}

		var probe sessionLineProbe
		if err := json.Unmarshal(line, &probe); err != nil {
			return session, fmt.Errorf(
				"backupplan: decode session line %d: %w",
				lineNumber,
				err,
			)
		}
		event, err := consumeSessionLine(
			line,
			lineNumber,
			sessionID,
			probe,
			&session,
			&headerSeen,
		)
		if err != nil {
			return session, err
		}
		if event != nil {
			if err := recordSessionEvent(&session, event, &previousSeq); err != nil {
				return session, err
			}
		}
	}
	if !headerSeen {
		return session, errors.New("backupplan: session is missing header")
	}
	return session, nil
}

func recordSessionEvent(session *Session, event *Event, previousSeq *uint64) error {
	if event.Seq <= *previousSeq {
		return fmt.Errorf(
			"backupplan: session seq %d is not greater than previous seq %d",
			event.Seq,
			*previousSeq,
		)
	}
	*previousSeq = event.Seq
	if event.Type == EventTerminal {
		session.Terminal = event
		return nil
	}
	session.Events = append(session.Events, *event)
	return nil
}

type sessionLineProbe struct {
	Type          EventType       `json:"type"`
	SchemaVersion json.RawMessage `json:"schemaVersion"`
}

func nextSessionLine(
	reader *bufio.Reader,
	lineNumber int,
) (line []byte, truncated bool, err error) {
	line, readErr := reader.ReadBytes('\n')
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, false, fmt.Errorf(
			"backupplan: read session line %d: %w",
			lineNumber,
			readErr,
		)
	}
	completeLine := len(line) > 0 && line[len(line)-1] == '\n'
	line = bytes.TrimSuffix(line, []byte{'\n'})
	return line, errors.Is(readErr, io.EOF) && !completeLine, nil
}

func consumeSessionLine(
	line []byte,
	lineNumber int,
	sessionID string,
	probe sessionLineProbe,
	session *Session,
	headerSeen *bool,
) (*Event, error) {
	if err := rejectDuplicateFoldedJSONFields(line); err != nil {
		return nil, fmt.Errorf("backupplan: session line %d: %w", lineNumber, err)
	}
	if probe.Type != "header" && probe.SchemaVersion != nil {
		return nil, errors.New(
			"backupplan: schemaVersion is only allowed in the session header",
		)
	}
	if probe.Type == "header" {
		return nil, consumeSessionHeader(
			line,
			lineNumber,
			sessionID,
			session,
			headerSeen,
		)
	}
	if !*headerSeen {
		return nil, errors.New("backupplan: session is missing header")
	}
	var wire eventWire
	if err := json.Unmarshal(line, &wire); err != nil {
		return nil, fmt.Errorf("backupplan: decode session event: %w", err)
	}
	if rule, exists := eventRules[wire.Type]; exists {
		if err := validateDecodedForbiddenEventFields(line, wire.Type, rule.allowed); err != nil {
			return nil, err
		}
	}
	event, err := eventFromWire(wire)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func consumeSessionHeader(
	line []byte,
	lineNumber int,
	sessionID string,
	session *Session,
	headerSeen *bool,
) error {
	if *headerSeen {
		return errors.New("backupplan: duplicate session header")
	}
	if lineNumber != 1 {
		return errors.New("backupplan: session header must be the first line")
	}
	var wire sessionHeaderWire
	if err := json.Unmarshal(line, &wire); err != nil {
		return fmt.Errorf("backupplan: decode session header: %w", err)
	}
	if wire.SchemaVersion != schemaVersion {
		return fmt.Errorf(
			"backupplan: unsupported session schemaVersion %d",
			wire.SchemaVersion,
		)
	}
	header, err := headerFromWire(wire)
	if err != nil {
		return err
	}
	if header.SessionID != sessionID {
		return fmt.Errorf(
			"backupplan: sessionID mismatch: filename is %q, header contains %q",
			sessionID,
			header.SessionID,
		)
	}
	session.Header = header
	*headerSeen = true
	return nil
}

func validateHeader(header Header) error {
	if err := validateULID("sessionID", header.SessionID); err != nil {
		return fmt.Errorf("backupplan: %w", err)
	}
	if err := validateText("executor.version", header.Executor.Version); err != nil {
		return err
	}
	if err := validateText("executor.host", header.Executor.Host); err != nil {
		return err
	}
	if err := validateTime("startedAt", header.StartedAt); err != nil {
		return err
	}
	readyPlans := make(map[string]struct{}, len(header.ReadyPlans))
	for _, planID := range header.ReadyPlans {
		if err := validateULID("ready planID", planID); err != nil {
			return fmt.Errorf("backupplan: %w", err)
		}
		if _, exists := readyPlans[planID]; exists {
			return fmt.Errorf("backupplan: duplicate ready planID %q", planID)
		}
		readyPlans[planID] = struct{}{}
	}
	return nil
}

func validateEvent(event Event) error {
	if event.Type == eventFileProgress {
		return errors.New("backupplan: file_progress is socket-only and must not be logged")
	}
	if err := validateTime("event at", event.At); err != nil {
		return err
	}
	if event.Seq == 0 {
		return errors.New("backupplan: event seq must be greater than zero")
	}

	rule, exists := eventRules[event.Type]
	if !exists {
		return fmt.Errorf("backupplan: unsupported session event type %q", event.Type)
	}
	if err := rule.validate(event); err != nil {
		return err
	}
	return validateForbiddenEventFields(event, rule.allowed)
}

type eventFields uint32

const (
	fieldPlanID eventFields = 1 << iota
	fieldCandidates
	fieldTapeID
	fieldVolumeID
	fieldResult
	fieldExtraSnapshots
	fieldDropped
	fieldMetadataRef
	fieldGeneration
	fieldBytes
	fieldFiles
	fieldPath
	fieldEntry
	fieldSnapshot
	fieldItemsWritten
	fieldItemsFailed
	fieldReason
	fieldDetail
	fieldState
	fieldPlansCompleted
)

type eventRule struct {
	allowed  eventFields
	validate func(Event) error
}

var eventRules = map[EventType]eventRule{
	EventWaitingForTape: {
		allowed:  fieldCandidates,
		validate: func(event Event) error { return validateCandidates(event.Candidates) },
	},
	EventTapeLoaded: {
		allowed: fieldTapeID | fieldVolumeID,
		validate: func(event Event) error {
			return validateTapeAndVolume(event.TapeID, event.VolumeID)
		},
	},
	EventTapeRejected: {
		allowed:  fieldTapeID | fieldReason | fieldDetail,
		validate: validateTapeRejected,
	},
	EventPlanStarted: {
		allowed:  fieldPlanID,
		validate: validatePlanIDEvent,
	},
	EventPlanCompleted: {
		allowed:  fieldPlanID | fieldItemsWritten | fieldItemsFailed,
		validate: validatePlanCompleted,
	},
	EventPlanFailed: {
		allowed:  fieldPlanID | fieldReason | fieldDetail,
		validate: validatePlanFailure,
	},
	EventPlanCancelled: {
		allowed:  fieldPlanID | fieldReason | fieldDetail,
		validate: validatePlanFailure,
	},
	EventEncryptionVerified: {
		allowed:  fieldPlanID,
		validate: validatePlanIDEvent,
	},
	EventSnapshotSwept: {
		allowed:  fieldEntry | fieldReason,
		validate: validateSnapshotSwept,
	},
	EventOutboxOverflow: {
		validate: func(Event) error { return nil },
	},
	EventDivergenceChecked: {
		allowed:  fieldPlanID | fieldResult | fieldExtraSnapshots,
		validate: validateDivergenceChecked,
	},
	EventFreshnessChecked: {
		allowed:  fieldPlanID | fieldDropped,
		validate: validateFreshnessChecked,
	},
	EventItemStarted: {
		allowed: fieldPlanID | fieldMetadataRef | fieldGeneration |
			fieldBytes | fieldFiles,
		validate: validateItemStarted,
	},
	EventItemCompleted: {
		allowed: fieldPlanID | fieldMetadataRef | fieldGeneration |
			fieldSnapshot,
		validate: validateItemCompleted,
	},
	EventItemFailed: {
		allowed: fieldPlanID | fieldMetadataRef | fieldGeneration |
			fieldReason | fieldDetail,
		validate: validateItemFailure,
	},
	EventItemSkipped: {
		allowed: fieldPlanID | fieldMetadataRef | fieldGeneration |
			fieldReason | fieldDetail,
		validate: validateItemFailure,
	},
	EventFileStarted: {
		allowed:  fieldPlanID | fieldMetadataRef | fieldBytes | fieldPath,
		validate: validateFileStarted,
	},
	EventFileCompleted: {
		allowed:  fieldPlanID | fieldMetadataRef | fieldPath,
		validate: validateFileCompleted,
	},
	EventCancelRequested: {
		allowed:  fieldPlanID,
		validate: validatePlanIDEvent,
	},
	EventTerminal: {
		allowed:  fieldState | fieldReason | fieldPlansCompleted,
		validate: validateTerminal,
	},
}

func validatePlanIDEvent(event Event) error {
	return validateEventPlanID(event.PlanID)
}

func validateCandidates(candidates []tape.ID) error {
	if len(candidates) == 0 {
		return errors.New("backupplan: candidates must contain at least one tapeID")
	}
	for _, candidate := range candidates {
		if _, err := tape.ParseID(string(candidate)); err != nil {
			return fmt.Errorf("backupplan: candidate %w", err)
		}
	}
	return nil
}

func validateTapeAndVolume(tapeID tape.ID, volumeID volume.ID) error {
	if _, err := tape.ParseID(string(tapeID)); err != nil {
		return fmt.Errorf("backupplan: %w", err)
	}
	if _, err := volume.ParseID(string(volumeID)); err != nil {
		return fmt.Errorf("backupplan: %w", err)
	}
	return nil
}

func validateTapeRejected(event Event) error {
	if _, err := tape.ParseID(string(event.TapeID)); err != nil {
		return fmt.Errorf("backupplan: %w", err)
	}
	if err := validateText("reason", event.Reason); err != nil {
		return err
	}
	return validateOptionalText("detail", event.Detail)
}

func validateEventPlanID(planID string) error {
	if err := validateULID("planID", planID); err != nil {
		return fmt.Errorf("backupplan: %w", err)
	}
	return nil
}

func validatePlanCompleted(event Event) error {
	if err := validateEventPlanID(event.PlanID); err != nil {
		return err
	}
	if event.ItemsWritten < 0 {
		return errors.New("backupplan: itemsWritten must not be negative")
	}
	if event.ItemsFailed < 0 {
		return errors.New("backupplan: itemsFailed must not be negative")
	}
	return nil
}

func validatePlanFailure(event Event) error {
	if err := validateEventPlanID(event.PlanID); err != nil {
		return err
	}
	if err := validateText("reason", event.Reason); err != nil {
		return err
	}
	return validateOptionalText("detail", event.Detail)
}

func validateDivergenceChecked(event Event) error {
	if err := validateEventPlanID(event.PlanID); err != nil {
		return err
	}
	if err := validateText("result", event.Result); err != nil {
		return err
	}
	return validateSnapshots(event.ExtraSnapshots)
}

func validateSnapshotSwept(event Event) error {
	if err := validateText("entry", event.Entry); err != nil {
		return err
	}
	return validateText("reason", event.Reason)
}

func validateFreshnessChecked(event Event) error {
	if err := validateEventPlanID(event.PlanID); err != nil {
		return err
	}
	for _, item := range event.Dropped {
		if _, err := snapshotname.Format(item.MetadataRef, item.Generation); err != nil {
			return fmt.Errorf(
				"backupplan: dropped item (%q, %d): %w",
				item.MetadataRef,
				item.Generation,
				err,
			)
		}
		if err := validateItemReason("dropped item reason", item.Reason); err != nil {
			return err
		}
	}
	return nil
}

func validateItemStarted(event Event) error {
	if err := validateEventItem(event); err != nil {
		return err
	}
	if event.Bytes < 0 {
		return errors.New("backupplan: bytes must not be negative")
	}
	if event.Files < 0 {
		return errors.New("backupplan: files must not be negative")
	}
	return nil
}

func validateItemCompleted(event Event) error {
	expected, err := validateEventItemSnapshot(event)
	if err != nil {
		return err
	}
	if event.Snapshot != expected {
		return fmt.Errorf(
			"backupplan: snapshot %q does not match item snapshot %q",
			event.Snapshot,
			expected,
		)
	}
	return nil
}

func validateItemFailure(event Event) error {
	if err := validateEventItem(event); err != nil {
		return err
	}
	if err := validateItemReason("reason", event.Reason); err != nil {
		return err
	}
	return validateOptionalText("detail", event.Detail)
}

func validateItemReason(field, reason string) error {
	if err := validateText(field, reason); err != nil {
		return err
	}
	switch ItemReason(reason) {
	case ReasonPayloadDrift,
		ReasonClaimStale,
		ReasonInsufficientSpace,
		ReasonChecksumMismatch,
		ReasonAlreadyPresent,
		ReasonUnsupportedFileType,
		ReasonSeriesMoved,
		ReasonSeriesRootMissing,
		ReasonStagedIntent,
		ReasonWriteFailed:
		return nil
	default:
		return fmt.Errorf("backupplan: unsupported item reason %q", reason)
	}
}

func validateEventItem(event Event) error {
	if err := validateEventPlanID(event.PlanID); err != nil {
		return err
	}
	if _, err := snapshotname.Format(event.MetadataRef, event.Generation); err != nil {
		return fmt.Errorf(
			"backupplan: event item (%q, %d): %w",
			event.MetadataRef,
			event.Generation,
			err,
		)
	}
	return nil
}

func validateEventItemSnapshot(event Event) (string, error) {
	if err := validateEventItem(event); err != nil {
		return "", err
	}
	if _, _, err := snapshotname.Parse(event.Snapshot); err != nil {
		return "", fmt.Errorf("backupplan: snapshot %q: %w", event.Snapshot, err)
	}
	snapshot, _ := snapshotname.Format(event.MetadataRef, event.Generation)
	return snapshot, nil
}

func validateFileStarted(event Event) error {
	if err := validateFileEvent(event); err != nil {
		return err
	}
	if event.Bytes < 0 {
		return errors.New("backupplan: bytes must not be negative")
	}
	return nil
}

func validateFileCompleted(event Event) error {
	return validateFileEvent(event)
}

func validateFileEvent(event Event) error {
	if err := validateEventPlanID(event.PlanID); err != nil {
		return err
	}
	if err := validateText("metadataRef", event.MetadataRef); err != nil {
		return err
	}
	return validateText("path", event.Path)
}

func validateTerminal(event Event) error {
	if err := validateText("state", event.State); err != nil {
		return err
	}
	if err := validateText("reason", event.Reason); err != nil {
		return err
	}
	if event.PlansCompleted < 0 {
		return errors.New("backupplan: plansCompleted must not be negative")
	}
	return nil
}

func validateOptionalText(field, value string) error {
	if value == "" {
		return nil
	}
	return validateText(field, value)
}

func validateForbiddenEventFields(event Event, allowed eventFields) error {
	// Event uses value fields, so zero values cannot be distinguished from
	// absence on encode. Decode validation separately checks raw key presence.
	wire := reflect.ValueOf(toEventWire(event))
	return visitEventWireFields(func(index int, name string, mask eventFields) error {
		if wire.Field(index).IsZero() {
			return nil
		}
		return rejectForbiddenEventField(event.Type, name, mask, allowed)
	})
}

func validateDecodedForbiddenEventFields(
	line []byte,
	eventType EventType,
	allowed eventFields,
) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		return fmt.Errorf("backupplan: decode session event fields: %w", err)
	}
	return visitEventWireFields(func(_ int, name string, mask eventFields) error {
		for key := range fields {
			if strings.EqualFold(key, name) {
				return rejectForbiddenEventField(eventType, name, mask, allowed)
			}
		}
		return nil
	})
}

func visitEventWireFields(
	visit func(index int, name string, mask eventFields) error,
) error {
	const baseFieldCount = 3
	wire := reflect.TypeFor[eventWire]()
	for index := baseFieldCount; index < wire.NumField(); index++ {
		field := wire.Field(index)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		mask := eventFields(1 << uint(index-baseFieldCount))
		if err := visit(index, name, mask); err != nil {
			return err
		}
	}
	return nil
}

func rejectForbiddenEventField(
	eventType EventType,
	name string,
	mask, allowed eventFields,
) error {
	if allowed&mask != 0 {
		return nil
	}
	return fmt.Errorf(
		"backupplan: %s event must not contain %s",
		eventType,
		name,
	)
}

func headerFromWire(wire sessionHeaderWire) (Header, error) {
	var startedAt time.Time
	if wire.StartedAt != "" {
		var err error
		startedAt, err = time.Parse(time.RFC3339, wire.StartedAt)
		if err != nil {
			return Header{}, fmt.Errorf("backupplan: parse startedAt: %w", err)
		}
	}
	header := Header{
		SessionID: wire.SessionID,
		Executor: Creator{
			Version: wire.Executor.Version,
			Host:    wire.Executor.Host,
		},
		StartedAt:  startedAt,
		ReadyPlans: copyStrings(wire.ReadyPlans),
	}
	if err := validateHeader(header); err != nil {
		return Header{}, err
	}
	return header, nil
}

func toEventWire(event Event) eventWire {
	var candidates []string
	if len(event.Candidates) > 0 {
		candidates = make([]string, 0, len(event.Candidates))
		for _, candidate := range event.Candidates {
			candidates = append(candidates, string(candidate))
		}
	}
	var dropped []droppedItemWire
	if len(event.Dropped) > 0 {
		dropped = make([]droppedItemWire, 0, len(event.Dropped))
		for _, item := range event.Dropped {
			dropped = append(dropped, droppedItemWire{
				MetadataRef: item.MetadataRef,
				Generation:  item.Generation,
				Reason:      item.Reason,
			})
		}
	}
	return eventWire{
		Type:           event.Type,
		At:             event.At.UTC().Format(time.RFC3339),
		Seq:            event.Seq,
		PlanID:         event.PlanID,
		Candidates:     candidates,
		TapeID:         string(event.TapeID),
		VolumeID:       string(event.VolumeID),
		Result:         event.Result,
		ExtraSnapshots: copyStrings(event.ExtraSnapshots),
		Dropped:        dropped,
		MetadataRef:    event.MetadataRef,
		Generation:     event.Generation,
		Bytes:          event.Bytes,
		Files:          event.Files,
		Path:           event.Path,
		Entry:          event.Entry,
		Snapshot:       event.Snapshot,
		ItemsWritten:   event.ItemsWritten,
		ItemsFailed:    event.ItemsFailed,
		Reason:         event.Reason,
		Detail:         event.Detail,
		State:          event.State,
		PlansCompleted: event.PlansCompleted,
	}
}

func eventFromWire(wire eventWire) (Event, error) {
	var at time.Time
	if wire.At != "" {
		var err error
		at, err = time.Parse(time.RFC3339, wire.At)
		if err != nil {
			return Event{}, fmt.Errorf("backupplan: parse event at: %w", err)
		}
	}
	var candidates []tape.ID
	if len(wire.Candidates) > 0 {
		candidates = make([]tape.ID, 0, len(wire.Candidates))
		for _, candidate := range wire.Candidates {
			candidates = append(candidates, tape.ID(candidate))
		}
	}
	var dropped []DroppedItem
	if len(wire.Dropped) > 0 {
		dropped = make([]DroppedItem, 0, len(wire.Dropped))
		for _, item := range wire.Dropped {
			dropped = append(dropped, DroppedItem{
				MetadataRef: item.MetadataRef,
				Generation:  item.Generation,
				Reason:      item.Reason,
			})
		}
	}
	event := Event{
		Type:           wire.Type,
		At:             at,
		Seq:            wire.Seq,
		PlanID:         wire.PlanID,
		Candidates:     candidates,
		TapeID:         tape.ID(wire.TapeID),
		VolumeID:       volume.ID(wire.VolumeID),
		Result:         wire.Result,
		ExtraSnapshots: copyStrings(wire.ExtraSnapshots),
		Dropped:        dropped,
		MetadataRef:    wire.MetadataRef,
		Generation:     wire.Generation,
		Bytes:          wire.Bytes,
		Files:          wire.Files,
		Path:           wire.Path,
		Entry:          wire.Entry,
		Snapshot:       wire.Snapshot,
		ItemsWritten:   wire.ItemsWritten,
		ItemsFailed:    wire.ItemsFailed,
		Reason:         wire.Reason,
		Detail:         wire.Detail,
		State:          wire.State,
		PlansCompleted: wire.PlansCompleted,
	}
	if err := validateEvent(event); err != nil {
		return Event{}, err
	}
	return event, nil
}

func sessionsDir(stateRoot string) string {
	return filepath.Join(stateRoot, "sessions")
}

func sessionFile(stateRoot, sessionID string) string {
	return filepath.Join(sessionsDir(stateRoot), sessionID+".jsonl")
}

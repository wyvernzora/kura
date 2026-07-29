// Package backupplan owns immutable backup plans and forensic session logs.
//
// A plan must exist in exactly one of plans/draft, plans/ready, plans/done,
// or plans/discarded at any time. Lifecycle transitions move the unchanged
// plan file forward: draft to ready to done, or draft/ready to discarded.
// There is no reverse transition.
package backupplan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/services/tape-backup/internal/snapshotname"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
	"golang.org/x/text/unicode/norm"
)

const schemaVersion = 1

// ActionType is the closed plan action vocabulary.
type ActionType string

const (
	ActionBackup          ActionType = "backup"
	ActionAssertVolume    ActionType = "assert_volume"
	ActionAssertInventory ActionType = "assert_inventory"
	ActionAssertFreeSpace ActionType = "assert_free_space"
	ActionAdmit           ActionType = "admit"
	ActionReformat        ActionType = "reformat"
	ActionImport          ActionType = "import"
	ActionVerify          ActionType = "verify"
)

type planState string

const (
	stateDraft     planState = "draft"
	stateReady     planState = "ready"
	stateDone      planState = "done"
	stateDiscarded planState = "discarded"
)

var planMutationMu sync.Mutex

// Creator records the build and host that authored a plan.
type Creator struct {
	Version string
	Host    string
}

// Target identifies the cartridge label the operator should load.
type Target struct {
	TapeID       tape.ID
	MediumSerial string
}

// Action is one ordered operation or assertion against the loaded cartridge.
type Action struct {
	Type               ActionType
	MetadataRef        string
	RootPath           string
	Generation         int
	PayloadFingerprint string
	Bytes              int64
	VolumeID           volume.ID
	Snapshots          []string
}

// Plan is immutable ordered intent for one cartridge.
type Plan struct {
	PlanID    string
	CreatedAt time.Time
	CreatedBy Creator
	Target    Target
	Actions   []Action
}

type planWire struct {
	SchemaVersion int          `json:"schemaVersion"`
	PlanID        string       `json:"planID"`
	CreatedAt     string       `json:"createdAt"`
	CreatedBy     creatorWire  `json:"createdBy"`
	Target        targetWire   `json:"target"`
	Actions       []actionWire `json:"actions"`
}

type creatorWire struct {
	Version string `json:"version"`
	Host    string `json:"host"`
}

type targetWire struct {
	TapeID       string `json:"tapeID"`
	MediumSerial string `json:"mediumSerial,omitempty"`
}

type actionWire struct {
	Type               ActionType `json:"type"`
	MetadataRef        *string    `json:"metadataRef,omitempty"`
	RootPath           *string    `json:"rootPath,omitempty"`
	Generation         *int       `json:"generation,omitempty"`
	PayloadFingerprint *string    `json:"payloadFingerprint,omitempty"`
	Bytes              *int64     `json:"bytes,omitempty"`
	VolumeID           *string    `json:"volumeID,omitempty"`
	Snapshots          *[]string  `json:"snapshots,omitempty"`
	present            map[string]json.RawMessage
}

func (wire *actionWire) UnmarshalJSON(data []byte) error {
	type plain actionWire
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var present map[string]json.RawMessage
	if err := json.Unmarshal(data, &present); err != nil {
		return err
	}
	*wire = actionWire(decoded)
	wire.present = present
	return nil
}

// Draft validates and writes a new immutable draft plan.
func Draft(stateRoot string, plan Plan) error {
	planMutationMu.Lock()
	defer planMutationMu.Unlock()

	if err := validateDraftPreconditions(stateRoot, plan); err != nil {
		return err
	}
	existingPlanID, err := livePlanForTape(stateRoot, plan.Target.TapeID)
	if err != nil {
		return fmt.Errorf("backupplan: draft plan %s: %w", plan.PlanID, err)
	}
	if existingPlanID != "" {
		return fmt.Errorf(
			"backupplan: tape %s already has live plan %s",
			plan.Target.TapeID,
			existingPlanID,
		)
	}

	data, err := json.MarshalIndent(toWire(plan), "", "  ")
	if err != nil {
		return fmt.Errorf("backupplan: encode plan %s: %w", plan.PlanID, err)
	}
	data = append(data, '\n')
	destinationDir := planStateDir(stateRoot, stateDraft)
	if err := os.MkdirAll(destinationDir, 0o775); err != nil {
		return fmt.Errorf("backupplan: create draft directory: %w", err)
	}
	temp, err := os.CreateTemp(destinationDir, ".plan-")
	if err != nil {
		return fmt.Errorf("backupplan: create temporary plan: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o664); err != nil {
		_ = temp.Close()
		return fmt.Errorf("backupplan: set temporary plan permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("backupplan: write temporary plan: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("backupplan: sync temporary plan: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("backupplan: close temporary plan: %w", err)
	}

	destination := planFile(stateRoot, stateDraft, plan.PlanID)
	// ensurePlanAbsent checked every lifecycle path under planMutationMu. This
	// repeated check improves the error if another process raced that check; it
	// does not make cross-process use safe.
	if exists, err := regularFileExists(destination); err != nil {
		return fmt.Errorf("backupplan: inspect draft destination: %w", err)
	} else if exists {
		return fmt.Errorf(
			"backupplan: draft plan %s: destination %q already exists",
			plan.PlanID,
			destination,
		)
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return fmt.Errorf("backupplan: draft plan %s: rename: %w", plan.PlanID, err)
	}
	return nil
}

func validateDraftPreconditions(stateRoot string, plan Plan) error {
	if err := validatePlan(plan); err != nil {
		return err
	}
	if err := ensurePlanStateDirectories(stateRoot); err != nil {
		return err
	}
	return ensurePlanAbsent(stateRoot, plan.PlanID)
}

// Approve atomically moves a plan from draft to ready.
func Approve(stateRoot, planID string) error {
	return movePlan(
		stateRoot,
		planID,
		"approve",
		stateDraft,
		stateReady,
		stateDone,
		stateDiscarded,
	)
}

// Complete atomically moves a plan from ready to done.
func Complete(stateRoot, planID string) error {
	return movePlan(
		stateRoot,
		planID,
		"complete",
		stateReady,
		stateDone,
		stateDraft,
		stateDiscarded,
	)
}

// Discard atomically moves a draft or ready plan to discarded.
func Discard(stateRoot, planID string) error {
	planMutationMu.Lock()
	defer planMutationMu.Unlock()

	if err := validateULID("planID", planID); err != nil {
		return fmt.Errorf("backupplan: %w", err)
	}
	if err := ensurePlanStateDirectories(stateRoot); err != nil {
		return err
	}

	present := make(map[planState]bool, 4)
	for _, state := range allPlanStates() {
		exists, err := regularFileExists(planFile(stateRoot, state, planID))
		if err != nil {
			return fmt.Errorf(
				"backupplan: discard %s: inspect %s state: %w",
				planID,
				state,
				err,
			)
		}
		if exists {
			present[state] = true
		}
	}

	var source planState
	switch {
	case present[stateDraft]:
		source = stateDraft
	case present[stateReady]:
		source = stateReady
	case present[stateDone] && present[stateDiscarded]:
		return fmt.Errorf(
			"backupplan: plan %s exists in multiple states: done, discarded",
			planID,
		)
	case present[stateDone]:
		return fmt.Errorf("backupplan: discard %s: done plan cannot be discarded", planID)
	case present[stateDiscarded]:
		return fmt.Errorf("backupplan: discard %s: plan is already discarded", planID)
	default:
		return fmt.Errorf(
			"backupplan: discard %s: draft or ready plan does not exist: %w",
			planID,
			os.ErrNotExist,
		)
	}

	return movePlanLocked(
		stateRoot,
		planID,
		"discard",
		source,
		stateDiscarded,
		stateDraft,
		stateReady,
		stateDone,
	)
}

// ReadDraft reads and validates one draft plan.
func ReadDraft(stateRoot, planID string) (Plan, error) {
	return readPlanState(stateRoot, planID, stateDraft)
}

// ReadReady reads and validates one ready plan.
func ReadReady(stateRoot, planID string) (Plan, error) {
	return readPlanState(stateRoot, planID, stateReady)
}

// ReadDone reads and validates one completed plan.
func ReadDone(stateRoot, planID string) (Plan, error) {
	return readPlanState(stateRoot, planID, stateDone)
}

// ListDraft returns all validated draft plans in plan-ID order.
func ListDraft(stateRoot string) ([]Plan, error) {
	return listPlanState(stateRoot, stateDraft)
}

// ListReady returns all validated ready plans in plan-ID order.
func ListReady(stateRoot string) ([]Plan, error) {
	return listPlanState(stateRoot, stateReady)
}

// ListDone returns all validated completed plans in plan-ID order.
func ListDone(stateRoot string) ([]Plan, error) {
	return listPlanState(stateRoot, stateDone)
}

func movePlan(
	stateRoot, planID, action string,
	sourceState, destinationState planState,
	otherStates ...planState,
) error {
	planMutationMu.Lock()
	defer planMutationMu.Unlock()

	return movePlanLocked(
		stateRoot,
		planID,
		action,
		sourceState,
		destinationState,
		otherStates...,
	)
}

func movePlanLocked(
	stateRoot, planID, action string,
	sourceState, destinationState planState,
	otherStates ...planState,
) error {
	if err := validateULID("planID", planID); err != nil {
		return fmt.Errorf("backupplan: %w", err)
	}
	if err := ensurePlanStateDirectories(stateRoot); err != nil {
		return err
	}
	source := planFile(stateRoot, sourceState, planID)
	if exists, err := regularFileExists(source); err != nil {
		return fmt.Errorf("backupplan: %s %s: inspect source: %w", action, planID, err)
	} else if !exists {
		return fmt.Errorf(
			"backupplan: %s %s: %s plan does not exist: %w",
			action,
			planID,
			sourceState,
			os.ErrNotExist,
		)
	}
	destinationDir := planStateDir(stateRoot, destinationState)
	if err := os.MkdirAll(destinationDir, 0o775); err != nil {
		return fmt.Errorf(
			"backupplan: %s %s: create %s directory: %w",
			action,
			planID,
			destinationState,
			err,
		)
	}
	destination := planFile(stateRoot, destinationState, planID)
	// Plans are files. os.Rename would silently replace an existing destination,
	// so this check is the guard that prevents loss of the destination plan.
	if exists, err := regularFileExists(destination); err != nil {
		return fmt.Errorf("backupplan: %s %s: inspect destination: %w", action, planID, err)
	} else if exists {
		return fmt.Errorf(
			"backupplan: %s %s: cannot move from %q to %q: %s plan already exists",
			action,
			planID,
			source,
			destination,
			destinationState,
		)
	}
	for _, otherState := range otherStates {
		if otherState == sourceState || otherState == destinationState {
			continue
		}
		other := planFile(stateRoot, otherState, planID)
		if exists, err := regularFileExists(other); err != nil {
			return fmt.Errorf("backupplan: %s %s: inspect %s state: %w", action, planID, otherState, err)
		} else if exists {
			return fmt.Errorf(
				"backupplan: plan %s exists in both %s and %s",
				planID,
				sourceState,
				otherState,
			)
		}
	}
	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("backupplan: %s %s: rename: %w", action, planID, err)
	}
	return nil
}

func readPlanState(stateRoot, planID string, state planState) (Plan, error) {
	planMutationMu.Lock()
	defer planMutationMu.Unlock()

	if err := validateULID("planID", planID); err != nil {
		return Plan{}, fmt.Errorf("backupplan: %w", err)
	}
	if err := ensureOnlyPlanState(stateRoot, planID, state); err != nil {
		return Plan{}, err
	}
	return readPlanFile(planFile(stateRoot, state, planID), planID)
}

func listPlanState(stateRoot string, state planState) ([]Plan, error) {
	planMutationMu.Lock()
	defer planMutationMu.Unlock()

	entries, err := os.ReadDir(planStateDir(stateRoot, state))
	if errors.Is(err, os.ErrNotExist) {
		return []Plan{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backupplan: list %s plans: %w", state, err)
	}
	plans := make([]Plan, 0, len(entries))
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		planID := strings.TrimSuffix(entry.Name(), ".json")
		if err := validateULID("planID", planID); err != nil {
			return nil, fmt.Errorf("backupplan: list %s plans: %w", state, err)
		}
		if err := ensureOnlyPlanState(stateRoot, planID, state); err != nil {
			return nil, err
		}
		plan, err := readPlanFile(planFile(stateRoot, state, planID), planID)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func ensurePlanAbsent(stateRoot, planID string) error {
	for _, state := range allPlanStates() {
		path := planFile(stateRoot, state, planID)
		exists, err := regularFileExists(path)
		if err != nil {
			return fmt.Errorf("backupplan: inspect %s plan %s: %w", state, planID, err)
		}
		if exists {
			return fmt.Errorf("backupplan: plan %s already exists in %s", planID, state)
		}
	}
	return nil
}

func ensureOnlyPlanState(stateRoot, planID string, expected planState) error {
	found := make([]planState, 0, 4)
	for _, state := range allPlanStates() {
		exists, err := regularFileExists(planFile(stateRoot, state, planID))
		if err != nil {
			return fmt.Errorf("backupplan: inspect %s plan %s: %w", state, planID, err)
		}
		if exists {
			found = append(found, state)
		}
	}
	if len(found) > 1 {
		return fmt.Errorf(
			"backupplan: plan %s exists in multiple states: %s",
			planID,
			joinPlanStates(found),
		)
	}
	if len(found) == 0 || found[0] != expected {
		return fmt.Errorf(
			"backupplan: read %s plan %s: %w",
			expected,
			planID,
			os.ErrNotExist,
		)
	}
	return nil
}

func livePlanForTape(stateRoot string, tapeID tape.ID) (string, error) {
	for _, state := range []planState{stateDraft, stateReady} {
		entries, err := os.ReadDir(planStateDir(stateRoot, state))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("backupplan: inspect live %s plans: %w", state, err)
		}
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			planID := strings.TrimSuffix(entry.Name(), ".json")
			if validateULID("planID", planID) != nil {
				continue
			}
			path := planFile(stateRoot, state, planID)
			plan, err := readPlanFile(path, planID)
			if err != nil {
				return "", fmt.Errorf(
					"inspect live %s plan %q: %w",
					state,
					path,
					err,
				)
			}
			if plan.Target.TapeID == tapeID {
				return plan.PlanID, nil
			}
		}
	}
	return "", nil
}

func readPlanFile(path, filenamePlanID string) (Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, fmt.Errorf("backupplan: read plan %s: %w", filenamePlanID, err)
	}
	return decodePlan(data, filenamePlanID)
}

func decodePlan(data []byte, filenamePlanID string) (Plan, error) {
	if !utf8.Valid(data) {
		return Plan{}, errors.New("backupplan: plan must be valid UTF-8")
	}
	var wire planWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return Plan{}, fmt.Errorf("backupplan: decode plan %s: %w", filenamePlanID, err)
	}
	if err := rejectDuplicateFoldedJSONFields(data); err != nil {
		return Plan{}, fmt.Errorf("backupplan: decode plan %s: %w", filenamePlanID, err)
	}
	if wire.SchemaVersion != schemaVersion {
		return Plan{}, fmt.Errorf(
			"backupplan: unsupported plan schemaVersion %d",
			wire.SchemaVersion,
		)
	}
	plan, err := fromWire(wire)
	if err != nil {
		return Plan{}, err
	}
	if plan.PlanID != filenamePlanID {
		return Plan{}, fmt.Errorf(
			"backupplan: planID mismatch: filename is %q, plan contains %q",
			filenamePlanID,
			plan.PlanID,
		)
	}
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func validatePlan(plan Plan) error {
	if err := validatePlanMetadata(plan); err != nil {
		return err
	}
	if err := validateTarget(plan.Target); err != nil {
		return err
	}
	if err := validateActions(plan.Actions); err != nil {
		return err
	}
	if planRequiresMediumSerial(plan.Actions) {
		return validateText("target.mediumSerial", plan.Target.MediumSerial)
	}
	return nil
}

func validatePlanMetadata(plan Plan) error {
	if err := validateULID("planID", plan.PlanID); err != nil {
		return fmt.Errorf("backupplan: %w", err)
	}
	if err := validateTime("createdAt", plan.CreatedAt); err != nil {
		return err
	}
	if err := validateText("createdBy.version", plan.CreatedBy.Version); err != nil {
		return err
	}
	if err := validateText("createdBy.host", plan.CreatedBy.Host); err != nil {
		return err
	}
	return nil
}

func validateTarget(target Target) error {
	if _, err := tape.ParseID(string(target.TapeID)); err != nil {
		return fmt.Errorf("backupplan: %w", err)
	}
	return nil
}

func validateActions(actions []Action) error {
	if len(actions) == 0 {
		return errors.New("backupplan: actions must contain at least one action")
	}

	backupKeys := make(map[string]struct{}, len(actions))
	for _, action := range actions {
		if err := validateAction(action); err != nil {
			return err
		}
		if action.Type != ActionBackup {
			continue
		}
		key := action.MetadataRef + "\x00" + fmt.Sprint(action.Generation)
		if _, exists := backupKeys[key]; exists {
			return fmt.Errorf(
				"backupplan: duplicate backup action (%q, %d)",
				action.MetadataRef,
				action.Generation,
			)
		}
		backupKeys[key] = struct{}{}
	}
	return nil
}

type actionFields uint16

const (
	actionFieldMetadataRef actionFields = 1 << iota
	actionFieldRootPath
	actionFieldGeneration
	actionFieldPayloadFingerprint
	actionFieldBytes
	actionFieldVolumeID
	actionFieldSnapshots
)

func validateAction(action Action) error {
	var allowed actionFields
	switch action.Type {
	case ActionBackup:
		allowed = actionFieldMetadataRef | actionFieldRootPath |
			actionFieldGeneration | actionFieldPayloadFingerprint | actionFieldBytes
		if err := validateBackupAction(action); err != nil {
			return err
		}
	case ActionAssertVolume, ActionAdmit:
		allowed = actionFieldVolumeID
		if _, err := volume.ParseID(string(action.VolumeID)); err != nil {
			return fmt.Errorf("backupplan: %w", err)
		}
	case ActionAssertInventory:
		allowed = actionFieldSnapshots
		if action.Snapshots == nil {
			return errors.New("backupplan: snapshots is required")
		}
		if err := validateSnapshots(action.Snapshots); err != nil {
			return err
		}
	case ActionAssertFreeSpace:
		allowed = actionFieldBytes
		if action.Bytes < 0 {
			return errors.New("backupplan: assert_free_space bytes must not be negative")
		}
	case ActionReformat, ActionImport, ActionVerify:
	default:
		if action.Type == "" {
			return errors.New("backupplan: action type is required")
		}
		return fmt.Errorf("backupplan: unsupported action type %q", action.Type)
	}
	return validateForbiddenActionFields(action, allowed)
}

func validateBackupAction(action Action) error {
	if _, err := snapshotname.Format(action.MetadataRef, action.Generation); err != nil {
		return fmt.Errorf(
			"backupplan: backup action (%q, %d): %w",
			action.MetadataRef,
			action.Generation,
			err,
		)
	}
	if err := validateRelativePath("rootPath", action.RootPath); err != nil {
		return err
	}
	if err := validateFingerprint(action.PayloadFingerprint); err != nil {
		return err
	}
	if action.Bytes < 0 {
		return fmt.Errorf(
			"backupplan: bytes for backup action (%q, %d) must not be negative",
			action.MetadataRef,
			action.Generation,
		)
	}
	return nil
}

func validateSnapshots(snapshots []string) error {
	seen := make(map[string]struct{}, len(snapshots))
	for _, name := range snapshots {
		if _, _, err := snapshotname.Parse(name); err != nil {
			return fmt.Errorf("backupplan: snapshot %q: %w", name, err)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("backupplan: duplicate snapshot %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validateRelativePath(field, value string) error {
	if value == "" {
		return fmt.Errorf("backupplan: %s is required", field)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("backupplan: %s %q is not valid UTF-8", field, value)
	}
	if strings.ContainsAny(value, "\x00\n") {
		return fmt.Errorf(
			"backupplan: %s %q must not contain NUL or newline",
			field,
			value,
		)
	}
	if path.IsAbs(value) {
		return fmt.Errorf("backupplan: %s %q must be relative", field, value)
	}
	for part := range strings.SplitSeq(value, "/") {
		if part == ".." {
			return fmt.Errorf("backupplan: %s %q must not contain ..", field, value)
		}
	}
	if path.Clean(value) != value || value == "." {
		return fmt.Errorf("backupplan: %s %q is not canonical", field, value)
	}
	if !norm.NFC.IsNormalString(value) {
		return fmt.Errorf("backupplan: %s %q must be NFC-normalized", field, value)
	}
	return nil
}

func validateForbiddenActionFields(action Action, allowed actionFields) error {
	value := reflect.ValueOf(action)
	actionType := reflect.TypeFor[Action]()
	for index := 1; index < actionType.NumField(); index++ {
		field := actionType.Field(index)
		if value.Field(index).IsZero() {
			continue
		}
		mask := actionFields(1 << uint(index-1))
		if allowed&mask == 0 {
			return fmt.Errorf(
				"backupplan: %s action must not contain %s",
				action.Type,
				actionFieldJSONName(field.Name),
			)
		}
	}
	return nil
}

func actionFieldJSONName(name string) string {
	return strings.ToLower(name[:1]) + name[1:]
}

func validateULID(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	parsed, err := ulid.ParseStrict(value)
	if err != nil || parsed.String() != value {
		return fmt.Errorf(
			"%s %q must be a 26-character uppercase Crockford base32 ULID",
			field,
			value,
		)
	}
	return nil
}

func validateTime(field string, value time.Time) error {
	if value.IsZero() {
		return fmt.Errorf("backupplan: %s is required", field)
	}
	if _, offset := value.Zone(); offset != 0 {
		return fmt.Errorf("backupplan: %s must be UTC", field)
	}
	if value.Nanosecond() != 0 {
		return fmt.Errorf("backupplan: %s must be truncated to whole seconds", field)
	}
	return nil
}

func validateText(field, value string) error {
	if value == "" {
		return fmt.Errorf("backupplan: %s is required", field)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("backupplan: %s must be valid UTF-8", field)
	}
	return nil
}

func validateFingerprint(value string) error {
	algorithm, digest, found := strings.Cut(value, ":")
	if !found {
		return errors.New("backupplan: payloadFingerprint must use algorithm:digest format")
	}
	if algorithm != "sha256" {
		return fmt.Errorf(
			"backupplan: unsupported payloadFingerprint algorithm %q",
			algorithm,
		)
	}
	if len(digest) != sha256.Size*2 {
		return errors.New(
			"backupplan: payloadFingerprint sha256 digest must be 64 lowercase hexadecimal characters",
		)
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || hex.EncodeToString(decoded) != digest {
		return errors.New(
			"backupplan: payloadFingerprint sha256 digest must be 64 lowercase hexadecimal characters",
		)
	}
	return nil
}

func toWire(plan Plan) planWire {
	actions := make([]actionWire, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		actions = append(actions, toActionWire(action))
	}
	return planWire{
		SchemaVersion: schemaVersion,
		PlanID:        plan.PlanID,
		CreatedAt:     plan.CreatedAt.UTC().Format(time.RFC3339),
		CreatedBy: creatorWire{
			Version: plan.CreatedBy.Version,
			Host:    plan.CreatedBy.Host,
		},
		Target: targetWire{
			TapeID:       string(plan.Target.TapeID),
			MediumSerial: plan.Target.MediumSerial,
		},
		Actions: actions,
	}
}

func fromWire(wire planWire) (Plan, error) {
	var createdAt time.Time
	if wire.CreatedAt != "" {
		var err error
		createdAt, err = time.Parse(time.RFC3339, wire.CreatedAt)
		if err != nil {
			return Plan{}, fmt.Errorf("backupplan: parse createdAt: %w", err)
		}
	}
	actions := make([]Action, 0, len(wire.Actions))
	for _, actionWire := range wire.Actions {
		action, err := actionFromWire(actionWire)
		if err != nil {
			return Plan{}, err
		}
		actions = append(actions, action)
	}
	return Plan{
		PlanID:    wire.PlanID,
		CreatedAt: createdAt,
		CreatedBy: Creator{
			Version: wire.CreatedBy.Version,
			Host:    wire.CreatedBy.Host,
		},
		Target: Target{
			TapeID:       tape.ID(wire.Target.TapeID),
			MediumSerial: wire.Target.MediumSerial,
		},
		Actions: actions,
	}, nil
}

func toActionWire(action Action) actionWire {
	wire := actionWire{Type: action.Type}
	switch action.Type {
	case ActionBackup:
		wire.MetadataRef = pointer(action.MetadataRef)
		wire.RootPath = pointer(action.RootPath)
		wire.Generation = pointer(action.Generation)
		wire.PayloadFingerprint = pointer(action.PayloadFingerprint)
		wire.Bytes = pointer(action.Bytes)
	case ActionAssertVolume, ActionAdmit:
		wire.VolumeID = pointer(string(action.VolumeID))
	case ActionAssertInventory:
		snapshots := copyStrings(action.Snapshots)
		if snapshots == nil {
			snapshots = []string{}
		}
		wire.Snapshots = &snapshots
	case ActionAssertFreeSpace:
		wire.Bytes = pointer(action.Bytes)
	}
	return wire
}

func actionFromWire(wire actionWire) (Action, error) {
	allowed, required, err := actionWireRule(wire.Type)
	if err != nil {
		return Action{}, err
	}
	for _, field := range actionWireFields() {
		raw, present := actionWireField(wire.present, field.name)
		if allowed&field.mask == 0 {
			if present {
				return Action{}, fmt.Errorf(
					"backupplan: %s action must not contain %s",
					wire.Type,
					field.name,
				)
			}
			continue
		}
		if required&field.mask != 0 && (!present || bytes.Equal(raw, []byte("null"))) {
			return Action{}, fmt.Errorf(
				"backupplan: %s action requires %s",
				wire.Type,
				field.name,
			)
		}
	}
	return actionFromWireValues(wire), nil
}

func actionFromWireValues(wire actionWire) Action {
	action := Action{Type: wire.Type}
	if wire.MetadataRef != nil {
		action.MetadataRef = *wire.MetadataRef
	}
	if wire.RootPath != nil {
		action.RootPath = *wire.RootPath
	}
	if wire.Generation != nil {
		action.Generation = *wire.Generation
	}
	if wire.PayloadFingerprint != nil {
		action.PayloadFingerprint = *wire.PayloadFingerprint
	}
	if wire.Bytes != nil {
		action.Bytes = *wire.Bytes
	}
	if wire.VolumeID != nil {
		action.VolumeID = volume.ID(*wire.VolumeID)
	}
	if wire.Snapshots != nil {
		action.Snapshots = copyStrings(*wire.Snapshots)
		if action.Snapshots == nil {
			action.Snapshots = []string{}
		}
	}
	return action
}

type actionWireFieldSpec struct {
	name string
	mask actionFields
}

func actionWireFields() []actionWireFieldSpec {
	return []actionWireFieldSpec{
		{name: "metadataRef", mask: actionFieldMetadataRef},
		{name: "rootPath", mask: actionFieldRootPath},
		{name: "generation", mask: actionFieldGeneration},
		{name: "payloadFingerprint", mask: actionFieldPayloadFingerprint},
		{name: "bytes", mask: actionFieldBytes},
		{name: "volumeID", mask: actionFieldVolumeID},
		{name: "snapshots", mask: actionFieldSnapshots},
	}
}

func actionWireRule(actionType ActionType) (allowed, required actionFields, err error) {
	switch actionType {
	case ActionBackup:
		allowed = actionFieldMetadataRef | actionFieldRootPath |
			actionFieldGeneration | actionFieldPayloadFingerprint | actionFieldBytes
		required = allowed
	case ActionAssertVolume, ActionAdmit:
		allowed = actionFieldVolumeID
		required = allowed
	case ActionAssertInventory:
		allowed = actionFieldSnapshots
		required = allowed
	case ActionAssertFreeSpace:
		allowed = actionFieldBytes
		required = allowed
	case ActionReformat, ActionImport, ActionVerify:
	default:
		if actionType == "" {
			return 0, 0, errors.New("backupplan: action type is required")
		}
		return 0, 0, fmt.Errorf("backupplan: unsupported action type %q", actionType)
	}
	return allowed, required, nil
}

func actionWireField(
	fields map[string]json.RawMessage,
	name string,
) (json.RawMessage, bool) {
	for key, value := range fields {
		if strings.EqualFold(key, name) {
			return value, true
		}
	}
	return nil, false
}

func rejectDuplicateFoldedJSONFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	return rejectDuplicateFoldedJSONValue(decoder, "")
}

func rejectDuplicateFoldedJSONValue(decoder *json.Decoder, fieldPath string) error {
	token, _ := decoder.Token()
	if token == json.Delim('[') {
		for index := 0; decoder.More(); index++ {
			arrayPath := fmt.Sprintf("%s[%d]", fieldPath, index)
			if err := rejectDuplicateFoldedJSONValue(decoder, arrayPath); err != nil {
				return err
			}
		}
		_, _ = decoder.Token()
		return nil
	}
	if token != json.Delim('{') {
		return nil
	}

	keys := make([]string, 0)
	for decoder.More() {
		token, _ := decoder.Token()
		key, isObjectKey := token.(string)
		if !isObjectKey {
			// Every caller decodes the document before scanning it, so this is
			// unreachable today. It reports rather than returning nil because
			// the scan stops here: the keys after this point go unexamined, and
			// claiming a document holds no duplicate would be a lie a later
			// caller could act on.
			return fmt.Errorf(
				"malformed JSON object key at %s",
				jsonFieldPath(fieldPath, "?"),
			)
		}
		for _, previous := range keys {
			if !strings.EqualFold(previous, key) {
				continue
			}
			first, second := previous, key
			if second < first {
				first, second = second, first
			}
			return fmt.Errorf(
				"duplicate case-folded JSON fields at %s: %q and %q",
				jsonFieldPath(fieldPath, second),
				first,
				second,
			)
		}
		keys = append(keys, key)
		if err := rejectDuplicateFoldedJSONValue(decoder, jsonFieldPath(fieldPath, key)); err != nil {
			return err
		}
	}
	_, _ = decoder.Token()
	return nil
}

func jsonFieldPath(parent, field string) string {
	if parent == "" {
		return field
	}
	return parent + "." + field
}

func pointer[T any](value T) *T {
	return &value
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%q is not a regular file", path)
	}
	return true, nil
}

func ensurePlanStateDirectories(stateRoot string) error {
	for _, state := range allPlanStates() {
		path := planStateDir(stateRoot, state)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf(
				"backupplan: inspect %s plan directory %q: %w",
				state,
				path,
				err,
			)
		}
		if !info.IsDir() {
			return fmt.Errorf(
				"backupplan: %s plan directory %q is not a directory",
				state,
				path,
			)
		}
	}
	return nil
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func planStateDir(stateRoot string, state planState) string {
	return filepath.Join(stateRoot, "plans", string(state))
}

func planFile(stateRoot string, state planState, planID string) string {
	return filepath.Join(planStateDir(stateRoot, state), planID+".json")
}

func allPlanStates() []planState {
	return []planState{stateDraft, stateReady, stateDone, stateDiscarded}
}

func planRequiresMediumSerial(actions []Action) bool {
	for _, action := range actions {
		if action.Type == ActionReformat || action.Type == ActionAdmit {
			return true
		}
	}
	return false
}

func joinPlanStates(states []planState) string {
	values := make([]string, 0, len(states))
	for _, state := range states {
		values = append(values, string(state))
	}
	return strings.Join(values, ", ")
}

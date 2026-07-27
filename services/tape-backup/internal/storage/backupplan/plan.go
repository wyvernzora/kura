// Package backupplan owns immutable backup plans and forensic session logs.
//
// A plan must exist in exactly one of plans/draft, plans/ready, or plans/done
// at any time. Lifecycle transitions move the unchanged plan file forward:
// draft to ready to done. There is no reverse transition.
package backupplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/services/tape-backup/internal/snapshotname"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

const schemaVersion = 1

// PlanType is the closed plan operation vocabulary.
type PlanType string

const (
	PlanTypeAdmit  PlanType = "admit"
	PlanTypeBackup PlanType = "backup"
	PlanTypeImport PlanType = "import"
	PlanTypeVerify PlanType = "verify"
)

type planState string

const (
	stateDraft planState = "draft"
	stateReady planState = "ready"
	stateDone  planState = "done"
)

var planMutationMu sync.Mutex

// Creator records the build and host that authored a plan.
type Creator struct {
	Version string
	Host    string
}

// Target pins the intended archive volume and its observed inventory.
type Target struct {
	VolumeID          volume.ID
	TapeID            tape.ID
	RequiredFreeBytes int64
	ExpectedSnapshots []string
}

// Item pins one series generation for execution.
type Item struct {
	MetadataRef        string
	Title              string
	Generation         int
	PayloadFingerprint string
	Bytes              int64
}

// Plan is immutable intent for one archive volume.
type Plan struct {
	PlanID    string
	Type      PlanType
	CreatedAt time.Time
	CreatedBy Creator
	Target    Target
	Items     []Item
}

type planWire struct {
	SchemaVersion int         `json:"schemaVersion"`
	PlanID        string      `json:"planID"`
	Type          PlanType    `json:"type"`
	CreatedAt     string      `json:"createdAt"`
	CreatedBy     creatorWire `json:"createdBy"`
	Target        targetWire  `json:"target"`
	Items         []itemWire  `json:"items"`
}

type creatorWire struct {
	Version string `json:"version"`
	Host    string `json:"host"`
}

type targetWire struct {
	VolumeID          string   `json:"volumeID"`
	TapeID            string   `json:"tapeID"`
	RequiredFreeBytes int64    `json:"requiredFreeBytes"`
	ExpectedSnapshots []string `json:"expectedSnapshots"`
}

type itemWire struct {
	MetadataRef        string `json:"metadataRef"`
	Title              string `json:"title"`
	Generation         int    `json:"generation"`
	PayloadFingerprint string `json:"payloadFingerprint"`
	Bytes              int64  `json:"bytes"`
}

// Draft validates and writes a new immutable draft plan.
func Draft(stateRoot string, plan Plan) error {
	planMutationMu.Lock()
	defer planMutationMu.Unlock()

	if err := validateDraftPreconditions(stateRoot, plan); err != nil {
		return err
	}
	existingPlanID, err := livePlanForVolume(stateRoot, plan.Target.VolumeID)
	if err != nil {
		return fmt.Errorf("backupplan: draft plan %s: %w", plan.PlanID, err)
	}
	if existingPlanID != "" {
		return fmt.Errorf(
			"backupplan: volume %s already has live plan %s",
			plan.Target.VolumeID,
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
	return movePlan(stateRoot, planID, "approve", stateDraft, stateReady, stateDone)
}

// Complete atomically moves a plan from ready to done.
func Complete(stateRoot, planID string) error {
	return movePlan(stateRoot, planID, "complete", stateReady, stateDone, stateDraft)
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
	sourceState, destinationState, otherState planState,
) error {
	planMutationMu.Lock()
	defer planMutationMu.Unlock()

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
	for _, state := range []planState{stateDraft, stateReady, stateDone} {
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
	found := make([]planState, 0, 3)
	for _, state := range []planState{stateDraft, stateReady, stateDone} {
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

func livePlanForVolume(stateRoot string, volumeID volume.ID) (string, error) {
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
			if plan.Target.VolumeID == volumeID {
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
	return validateItems(
		plan.Target.RequiredFreeBytes,
		plan.Target.ExpectedSnapshots,
		plan.Items,
	)
}

func validatePlanMetadata(plan Plan) error {
	if err := validateULID("planID", plan.PlanID); err != nil {
		return fmt.Errorf("backupplan: %w", err)
	}
	switch plan.Type {
	case PlanTypeAdmit, PlanTypeBackup, PlanTypeImport, PlanTypeVerify:
	default:
		return fmt.Errorf("backupplan: unsupported plan type %q", plan.Type)
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
	if _, err := volume.ParseID(string(target.VolumeID)); err != nil {
		return fmt.Errorf("backupplan: %w", err)
	}
	if _, err := tape.ParseID(string(target.TapeID)); err != nil {
		return fmt.Errorf("backupplan: %w", err)
	}
	if target.RequiredFreeBytes < 0 {
		return errors.New("backupplan: requiredFreeBytes must not be negative")
	}
	names := make(map[string]struct{}, len(target.ExpectedSnapshots))
	for _, name := range target.ExpectedSnapshots {
		if _, _, err := snapshotname.Parse(name); err != nil {
			return fmt.Errorf("backupplan: expected snapshot %q: %w", name, err)
		}
		if _, exists := names[name]; exists {
			return fmt.Errorf("backupplan: duplicate expected snapshot %q", name)
		}
		names[name] = struct{}{}
	}
	return nil
}

func validateItems(
	requiredFreeBytes int64,
	expectedSnapshots []string,
	items []Item,
) error {
	if len(items) == 0 {
		return errors.New("backupplan: items must contain at least one item")
	}

	expected := make(map[string]struct{}, len(expectedSnapshots))
	for _, name := range expectedSnapshots {
		expected[name] = struct{}{}
	}
	keys := make(map[string]struct{}, len(items))
	var itemBytes int64
	for _, item := range items {
		snapshot, err := validateItem(item)
		if err != nil {
			return err
		}
		if _, exists := expected[snapshot]; exists {
			return fmt.Errorf(
				"backupplan: item (%q, %d) snapshot %q already exists on target",
				item.MetadataRef,
				item.Generation,
				snapshot,
			)
		}
		if item.Bytes > math.MaxInt64-itemBytes {
			return errors.New("backupplan: requiredFreeBytes overflow")
		}
		itemBytes += item.Bytes
		key := item.MetadataRef + "\x00" + fmt.Sprint(item.Generation)
		if _, exists := keys[key]; exists {
			return fmt.Errorf(
				"backupplan: duplicate item (%q, %d)",
				item.MetadataRef,
				item.Generation,
			)
		}
		keys[key] = struct{}{}
	}
	if requiredFreeBytes != itemBytes {
		return fmt.Errorf(
			"backupplan: requiredFreeBytes is %d, want sum of item bytes %d",
			requiredFreeBytes,
			itemBytes,
		)
	}
	return nil
}

func validateItem(item Item) (string, error) {
	snapshot, err := snapshotname.Format(item.MetadataRef, item.Generation)
	if err != nil {
		return "", fmt.Errorf(
			"backupplan: item (%q, %d): %w",
			item.MetadataRef,
			item.Generation,
			err,
		)
	}
	if err := validateText("item title", item.Title); err != nil {
		return "", err
	}
	if err := validateFingerprint(item.PayloadFingerprint); err != nil {
		return "", err
	}
	if item.Bytes < 0 {
		return "", fmt.Errorf(
			"backupplan: bytes for (%q, %d) must not be negative",
			item.MetadataRef,
			item.Generation,
		)
	}
	return snapshot, nil
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
	items := make([]itemWire, 0, len(plan.Items))
	for _, item := range plan.Items {
		items = append(items, itemWire{
			MetadataRef:        item.MetadataRef,
			Title:              item.Title,
			Generation:         item.Generation,
			PayloadFingerprint: item.PayloadFingerprint,
			Bytes:              item.Bytes,
		})
	}
	return planWire{
		SchemaVersion: schemaVersion,
		PlanID:        plan.PlanID,
		Type:          plan.Type,
		CreatedAt:     plan.CreatedAt.UTC().Format(time.RFC3339),
		CreatedBy: creatorWire{
			Version: plan.CreatedBy.Version,
			Host:    plan.CreatedBy.Host,
		},
		Target: targetWire{
			VolumeID:          string(plan.Target.VolumeID),
			TapeID:            string(plan.Target.TapeID),
			RequiredFreeBytes: plan.Target.RequiredFreeBytes,
			ExpectedSnapshots: copyStrings(plan.Target.ExpectedSnapshots),
		},
		Items: items,
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
	items := make([]Item, 0, len(wire.Items))
	for _, item := range wire.Items {
		items = append(items, Item{
			MetadataRef:        item.MetadataRef,
			Title:              item.Title,
			Generation:         item.Generation,
			PayloadFingerprint: item.PayloadFingerprint,
			Bytes:              item.Bytes,
		})
	}
	return Plan{
		PlanID:    wire.PlanID,
		Type:      wire.Type,
		CreatedAt: createdAt,
		CreatedBy: Creator{
			Version: wire.CreatedBy.Version,
			Host:    wire.CreatedBy.Host,
		},
		Target: Target{
			VolumeID:          volume.ID(wire.Target.VolumeID),
			TapeID:            tape.ID(wire.Target.TapeID),
			RequiredFreeBytes: wire.Target.RequiredFreeBytes,
			ExpectedSnapshots: copyStrings(wire.Target.ExpectedSnapshots),
		},
		Items: items,
	}, nil
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
	for _, state := range []planState{stateDraft, stateReady, stateDone} {
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

func joinPlanStates(states []planState) string {
	values := make([]string, 0, len(states))
	for _, state := range states {
		values = append(values, string(state))
	}
	return strings.Join(values, ", ")
}

// Package planfile owns reading and writing reconcile plan JSONL files at
// <library>/<series>/.kura/reconcile/<ulid>.jsonl.
//
// Line 1 of each file is the immutable plan record (PlanRecord). Lines 2..N
// are append-only events: one per attempted move plus one terminating
// result. Apply opens the log once, appends N+1 lines, closes; readers scan
// later to determine whether the plan was applied successfully.
//
// The exported PlanRecord/Change/Event types mirror the on-disk shape with
// native Go fields. Phase 7 of refactor.md may lift the plan-record schema
// into domain/reconcile/ once the redesign is locked.
package planfile

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/renameio/v2"
	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/textnorm"
)

const (
	currentSchemaVersion = 1
	kuraDir              = ".kura"
	dirName              = "reconcile"
)

// PlanRecord is line 1 of the JSONL file: the immutable plan to apply.
type PlanRecord struct {
	Token       string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Series      refs.Series
	MetadataRef refs.Metadata
	FileTitle   textnorm.NFCString
	Snapshot    string
	Changes     []Change
}

type Change struct {
	Kind       string
	Episode    refs.Episode
	From       string
	To         string
	Source     string
	Resolution string
	Companions []FileMove
	Replaced   *Replaced
}

type FileMove struct {
	From string
	To   string
}

type Replaced struct {
	From       string
	To         string
	Source     string
	Resolution string
	Companions []FileMove
}

// WritePlan creates the JSONL file with PlanRecord as line 1. The file is
// written atomically; subsequent events get appended via OpenLog.
func WritePlan(root string, ref refs.Series, p PlanRecord) error {
	if err := validateToken(p.Token); err != nil {
		return err
	}
	wire := planRecordToWire(p)
	data, err := json.Marshal(wire)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := planPath(root, ref, p.Token)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return renameio.WriteFile(path, data, 0o644)
}

// ReadPlan returns the plan from line 1 plus a flag reporting whether any
// later "result" line indicates a successful apply.
func ReadPlan(root string, ref refs.Series, token string) (PlanRecord, bool, error) {
	if err := validateToken(token); err != nil {
		return PlanRecord{}, false, err
	}
	file, err := os.Open(planPath(root, ref, token))
	if err != nil {
		return PlanRecord{}, false, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	line, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return PlanRecord{}, false, err
	}
	if len(line) == 0 {
		return PlanRecord{}, false, fmt.Errorf("planfile: %s is empty", token)
	}
	var wire planRecordV1
	if err := json.Unmarshal(line, &wire); err != nil {
		return PlanRecord{}, false, fmt.Errorf("planfile: decode %s: %w", token, err)
	}
	if wire.Type != "plan" {
		return PlanRecord{}, false, fmt.Errorf("planfile: %s line 1 has type %q", token, wire.Type)
	}
	if wire.SchemaVersion != currentSchemaVersion {
		return PlanRecord{}, false, fmt.Errorf("planfile: unsupported schemaVersion %d", wire.SchemaVersion)
	}
	plan, err := planRecordFromWire(wire)
	if err != nil {
		return PlanRecord{}, false, err
	}
	if plan.Token != token {
		return PlanRecord{}, false, fmt.Errorf("planfile: %s token mismatch (file contains %s)", token, plan.Token)
	}
	applied, err := scanForSuccess(reader)
	if err != nil {
		return PlanRecord{}, false, err
	}
	return plan, applied, nil
}

// ListTokens returns the ULIDs of every plan file under the series.
func ListTokens(root string, ref refs.Series) ([]string, error) {
	dir := filepath.Join(root, filepath.FromSlash(ref.String()), kuraDir, dirName)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tokens := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		token, ok := tokenFromFilename(name)
		if !ok {
			continue
		}
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	return tokens, nil
}

// Log is an open append handle to a plan's JSONL file. Apply opens once at
// the start, appends move events as it works through the plan, then appends
// a final result event before closing.
type Log struct {
	file    *os.File
	encoder *json.Encoder
}

func OpenLog(root string, ref refs.Series, token string) (*Log, error) {
	if err := validateToken(token); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(planPath(root, ref, token), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, err
	}
	return &Log{file: file, encoder: json.NewEncoder(file)}, nil
}

func (l *Log) Close() error {
	return l.file.Close()
}

// AppendMove records one move attempt. moveErr is the result of the move
// (nil for success); when non-nil, callers typically follow with
// AppendResult("failure", ...).
func (l *Log) AppendMove(at time.Time, phase string, index, total int, move FileMove, moveErr error) error {
	record := eventV1{
		Type:          "event",
		SchemaVersion: currentSchemaVersion,
		At:            at.UTC().Format(time.RFC3339),
		Phase:         phase,
		Index:         index,
		Total:         total,
		Move:          fileMoveToWire(move),
	}
	if moveErr != nil {
		record.Error = moveErr.Error()
	}
	return l.encoder.Encode(record)
}

// AppendResult records the terminal result. Status is "success" or
// "failure"; appliedMoves is the count of moves that completed before the
// result was written.
func (l *Log) AppendResult(at time.Time, status string, appliedMoves int, applyErr error) error {
	record := resultV1{
		Type:          "result",
		SchemaVersion: currentSchemaVersion,
		At:            at.UTC().Format(time.RFC3339),
		Status:        status,
		AppliedMoves:  appliedMoves,
	}
	if applyErr != nil {
		record.Error = applyErr.Error()
	}
	return l.encoder.Encode(record)
}

// Path returns the absolute path to a plan's JSONL file. Exported so
// reconcile callers can format paths in error messages and tests can stat
// the file directly.
func Path(root string, ref refs.Series, token string) (string, error) {
	if err := validateToken(token); err != nil {
		return "", err
	}
	return planPath(root, ref, token), nil
}

func planPath(root string, ref refs.Series, token string) string {
	return filepath.Join(root, filepath.FromSlash(ref.String()), kuraDir, dirName, token+".jsonl")
}

func tokenFromFilename(name string) (string, bool) {
	const ext = ".jsonl"
	if len(name) <= len(ext) || name[len(name)-len(ext):] != ext {
		return "", false
	}
	token := name[:len(name)-len(ext)]
	if _, err := ulid.ParseStrict(token); err != nil {
		return "", false
	}
	return token, true
}

func validateToken(token string) error {
	if _, err := ulid.ParseStrict(token); err != nil {
		return fmt.Errorf("planfile: invalid token %q", token)
	}
	return nil
}

func scanForSuccess(r io.Reader) (bool, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var header struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(line, &header); err != nil {
			return false, err
		}
		if header.Type == "result" && header.Status == "success" {
			return true, nil
		}
	}
	return false, scanner.Err()
}

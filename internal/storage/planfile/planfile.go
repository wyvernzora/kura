// Package planfile owns reading and writing reconcile plan JSONL files at
// <library>/<series>/.kura/reconcile/<ulid>.jsonl.
//
// Line 1 of each file is the immutable plan record (PlanRecord). Lines 2..N
// are append-only events: one per attempted move plus one terminating
// result. Apply opens the log once, appends N+1 lines, closes; readers scan
// later to determine whether the plan was applied successfully.
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
	"strings"
	"time"

	"github.com/google/renameio/v2/maybe"
	"github.com/wyvernzora/kura/internal/domain/reconcile"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

const currentSchemaVersion = 1

// PlanRecord is line 1 of the JSONL file: persistence envelope plus the
// immutable plan to apply.
type PlanRecord struct {
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
	Plan      reconcile.Plan
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
	path := paths.PlanFile(root, ref, p.Token)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return maybe.WriteFile(path, data, 0o644)
}

// ReadPlan returns the plan from line 1 plus a flag reporting whether any
// later "result" line indicates a successful apply.
func ReadPlan(root string, ref refs.Series, token string) (PlanRecord, bool, error) {
	if err := validateToken(token); err != nil {
		return PlanRecord{}, false, err
	}
	file, err := os.Open(paths.PlanFile(root, ref, token))
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
	dir := paths.PlanDir(root, ref)
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
// the start, appends one event per move as it works, then appends a final
// result event before closing.
type Log struct {
	file    *os.File
	encoder *json.Encoder
}

// OpenLog opens the plan file for append.
func OpenLog(root string, ref refs.Series, token string) (*Log, error) {
	if err := validateToken(token); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(paths.PlanFile(root, ref, token), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, err
	}
	return &Log{file: file, encoder: json.NewEncoder(file)}, nil
}

// Close releases the file handle.
func (l *Log) Close() error {
	return l.file.Close()
}

// AppendMove records one move attempt. moveErr is the result of the move
// (nil on success).
func (l *Log) AppendMove(at time.Time, index, total int, move reconcile.FileMove, moveErr error) error {
	record := eventV1{
		Type:          "event",
		SchemaVersion: currentSchemaVersion,
		At:            at.UTC().Format(time.RFC3339),
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

// TokenLength is the canonical length of a reconcile plan token: a 12-char
// lowercase hex prefix of the snapshot sha256. Fixed length so filename
// scanning and CLI input share one validator.
const TokenLength = 12

func tokenFromFilename(name string) (string, bool) {
	if !strings.HasSuffix(name, paths.PlanExtension) {
		return "", false
	}
	token := strings.TrimSuffix(name, paths.PlanExtension)
	if !validToken(token) {
		return "", false
	}
	return token, true
}

func validateToken(token string) error {
	if !validToken(token) {
		return fmt.Errorf("planfile: invalid token %q (want %d-char lowercase hex)", token, TokenLength)
	}
	return nil
}

func validToken(token string) bool {
	if len(token) != TokenLength {
		return false
	}
	for _, r := range token {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
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

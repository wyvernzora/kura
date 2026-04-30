// Package planfile owns reading and writing reconcile plan JSONL files
// at <library>/<series>/.kura/reconcile/<token>.jsonl.
//
// Schema v2 (current):
//   - Line 1: header (type:"header") with token / lifetime / series /
//     snapshot.
//   - Lines 2..N+1: steps (type:"step") — fully-unrolled file_move /
//     dir_remove operations with deterministic IDs and owner metadata.
//   - Lines N+2..M: events (type:"event"), one per attempted step.
//     Apply opens the file for append, writes events as it goes, and
//     terminates with a single result line (type:"result").
//
// Plan content is immutable post-WritePlan; only events + result are
// appended afterwards.
package planfile

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/renameio/v2/maybe"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/reconcile"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

const currentSchemaVersion = 2

// TokenLength is the canonical length of a reconcile plan token.
const TokenLength = reconcile.TokenLength

// WritePlan creates the JSONL file with one header line + N step
// lines. Atomically written via renameio.WriteFile; subsequent events
// get appended via OpenLog.
func WritePlan(root string, ref refs.Series, plan reconcile.Plan) error {
	if err := validateToken(plan.Header.Token); err != nil {
		return err
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(headerToWire(plan.Header)); err != nil {
		return err
	}
	for _, step := range plan.Steps {
		if err := enc.Encode(stepToWire(step)); err != nil {
			return err
		}
	}
	path := paths.PlanFile(root, ref, plan.Header.Token)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return maybe.WriteFile(path, []byte(buf.String()), 0o644)
}

// ReadPlan returns the plan from the JSONL file plus a flag reporting
// whether the apply log already contains a successful result line.
func ReadPlan(root string, ref refs.Series, token string) (reconcile.Plan, bool, error) {
	if err := validateToken(token); err != nil {
		return reconcile.Plan{}, false, err
	}
	file, err := os.Open(paths.PlanFile(root, ref, token))
	if err != nil {
		return reconcile.Plan{}, false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return reconcile.Plan{}, false, fmt.Errorf("planfile: read %s: %w", token, err)
		}
		return reconcile.Plan{}, false, fmt.Errorf("planfile: %s is empty", token)
	}
	var hw headerV2
	if err := json.Unmarshal(scanner.Bytes(), &hw); err != nil {
		return reconcile.Plan{}, false, fmt.Errorf("planfile: decode header %s: %w", token, err)
	}
	header, err := headerFromWire(hw)
	if err != nil {
		return reconcile.Plan{}, false, err
	}
	if header.Token != token {
		return reconcile.Plan{}, false, fmt.Errorf("planfile: %s token mismatch (file contains %s)", token, header.Token)
	}

	// Collect remaining lines so we can distinguish a torn trailing write
	// (last line fails to decode) from mid-file corruption (hard error).
	var rawLines [][]byte
	for scanner.Scan() {
		b := scanner.Bytes()
		cp := make([]byte, len(b))
		copy(cp, b)
		rawLines = append(rawLines, cp)
	}
	if err := scanner.Err(); err != nil {
		return reconcile.Plan{}, false, err
	}

	var steps []reconcile.Step
	applied := false
	for i, line := range rawLines {
		var head struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			if i == len(rawLines)-1 {
				// Torn trailing line from a crash mid-write — treat as no result yet.
				break
			}
			return reconcile.Plan{}, false, fmt.Errorf("planfile: decode line: %w", err)
		}
		switch head.Type {
		case "step":
			var sw stepV2
			if err := json.Unmarshal(line, &sw); err != nil {
				return reconcile.Plan{}, false, fmt.Errorf("planfile: decode step: %w", err)
			}
			step, err := stepFromWire(sw)
			if err != nil {
				return reconcile.Plan{}, false, err
			}
			steps = append(steps, step)
		case "event":
			// progress data; ignored by ReadPlan.
		case "result":
			if head.Status == "success" {
				applied = true
			}
		default:
			return reconcile.Plan{}, false, fmt.Errorf("planfile: unknown line type %q", head.Type)
		}
	}
	return reconcile.Plan{Header: header, Steps: steps}, applied, nil
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
		token, ok := tokenFromFilename(entry.Name())
		if !ok {
			continue
		}
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	return tokens, nil
}

// Log is an open append handle to a plan's JSONL file. Apply opens
// once, appends one event per attempted step, then a single result
// before closing.
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

// AppendEvent records one step attempt's outcome.
func (l *Log) AppendEvent(at time.Time, stepID string, stepErr error) error {
	rec := eventV2{
		Type: "event",
		At:   at.UTC().Format(time.RFC3339),
		Step: stepID,
	}
	if stepErr != nil {
		rec.Error = stepErr.Error()
	}
	if err := l.encoder.Encode(rec); err != nil {
		return err
	}
	return l.file.Sync()
}

// AppendResult records the terminal apply outcome.
func (l *Log) AppendResult(at time.Time, status string, appliedSteps int, applyErr error) error {
	rec := resultV2{
		Type:         "result",
		At:           at.UTC().Format(time.RFC3339),
		Status:       status,
		AppliedSteps: appliedSteps,
	}
	if applyErr != nil {
		rec.Error = applyErr.Error()
	}
	if err := l.encoder.Encode(rec); err != nil {
		return err
	}
	return l.file.Sync()
}

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

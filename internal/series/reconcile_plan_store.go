package series

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/renameio/v2"
	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series/wire"
	"github.com/wyvernzora/kura/internal/textnorm"
)

const reconcilePlanTTL = 5 * time.Minute

type StoredReconcilePlan struct {
	Token     string        `json:"token"`
	CreatedAt time.Time     `json:"createdAt"`
	ExpiresAt time.Time     `json:"expiresAt"`
	Plan      ReconcilePlan `json:"plan"`
}

type ReconcilePlanExpiredError struct {
	Token     string
	ExpiresAt time.Time
}

func (err ReconcilePlanExpiredError) Error() string {
	return fmt.Sprintf("series: reconcile plan %s expired at %s", err.Token, err.ExpiresAt.Format(time.RFC3339))
}

type ReconcilePlanAlreadyAppliedError struct {
	Token string
}

func (err ReconcilePlanAlreadyAppliedError) Error() string {
	return fmt.Sprintf("series: reconcile plan %s was already applied", err.Token)
}

func (h Handle) CreateReconcilePlan() (StoredReconcilePlan, error) {
	plan, metadataRef, err := h.planReconcile()
	if err != nil {
		return StoredReconcilePlan{}, err
	}
	if !plan.HasChanges() {
		return StoredReconcilePlan{Plan: plan}, nil
	}
	token := ulid.Make().String()
	createdAt := h.now().UTC()
	stored := StoredReconcilePlan{
		Token:     token,
		CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(reconcilePlanTTL),
		Plan:      plan,
	}
	record := wire.ReconcilePlanRecordV1{
		Type:          "plan",
		SchemaVersion: wire.CurrentSchemaVersion,
		Token:         token,
		CreatedAt:     stored.CreatedAt.Format(time.RFC3339),
		ExpiresAt:     stored.ExpiresAt.Format(time.RFC3339),
		Series:        h.ref.String(),
		MetadataRef:   metadataRef.String(),
		Plan:          toWireReconcilePlan(plan),
	}
	data, err := json.Marshal(record)
	if err != nil {
		return StoredReconcilePlan{}, err
	}
	data = append(data, '\n')
	path, err := h.reconcilePlanPath(token)
	if err != nil {
		return StoredReconcilePlan{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return StoredReconcilePlan{}, err
	}
	if err := renameio.WriteFile(path, data, 0o644); err != nil {
		return StoredReconcilePlan{}, err
	}
	return stored, nil
}

func (h Handle) loadStoredReconcilePlan(token string) (StoredReconcilePlan, bool, error) {
	path, err := h.reconcilePlanPath(token)
	if err != nil {
		return StoredReconcilePlan{}, false, err
	}
	file, err := os.Open(path)
	if err != nil {
		return StoredReconcilePlan{}, false, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	line, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return StoredReconcilePlan{}, false, err
	}
	if len(line) == 0 {
		return StoredReconcilePlan{}, false, fmt.Errorf("series: reconcile plan %s is empty", token)
	}
	var record wire.ReconcilePlanRecordV1
	if err := json.Unmarshal(line, &record); err != nil {
		return StoredReconcilePlan{}, false, fmt.Errorf("series: decode reconcile plan %s: %w", token, err)
	}
	stored, err := h.fromWireReconcilePlanRecord(record)
	if err != nil {
		return StoredReconcilePlan{}, false, err
	}
	if stored.Token != token {
		return StoredReconcilePlan{}, false, fmt.Errorf("series: reconcile plan token mismatch: file %s contains %s", token, stored.Token)
	}
	applied, err := reconcilePlanHasSuccess(reader)
	if err != nil {
		return StoredReconcilePlan{}, false, err
	}
	return stored, applied, nil
}

func reconcilePlanHasSuccess(r io.Reader) (bool, error) {
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

func (h Handle) fromWireReconcilePlanRecord(record wire.ReconcilePlanRecordV1) (StoredReconcilePlan, error) {
	if record.Type != "plan" {
		return StoredReconcilePlan{}, fmt.Errorf("series: reconcile plan record has type %q", record.Type)
	}
	if record.SchemaVersion != wire.CurrentSchemaVersion {
		return StoredReconcilePlan{}, fmt.Errorf("unsupported reconcile plan schemaVersion %d", record.SchemaVersion)
	}
	if record.Series != h.ref.String() {
		seriesRef, err := refs.ParseSeries(record.Series)
		if err != nil {
			return StoredReconcilePlan{}, err
		}
		return StoredReconcilePlan{}, PlanStaleError{Series: seriesRef}
	}
	metadataRef, err := refs.ParseMetadata(record.MetadataRef)
	if err != nil {
		return StoredReconcilePlan{}, err
	}
	series, err := h.load()
	if err != nil {
		return StoredReconcilePlan{}, err
	}
	if metadataRef != series.Metadata {
		return StoredReconcilePlan{}, PlanStaleError{Series: h.ref}
	}
	token, err := parseReconcilePlanToken(record.Token)
	if err != nil {
		return StoredReconcilePlan{}, err
	}
	createdAt, err := time.Parse(time.RFC3339, record.CreatedAt)
	if err != nil {
		return StoredReconcilePlan{}, fmt.Errorf("series: invalid reconcile plan createdAt %q: %w", record.CreatedAt, err)
	}
	expiresAt, err := time.Parse(time.RFC3339, record.ExpiresAt)
	if err != nil {
		return StoredReconcilePlan{}, fmt.Errorf("series: invalid reconcile plan expiresAt %q: %w", record.ExpiresAt, err)
	}
	plan, err := fromWireReconcilePlan(record.Plan)
	if err != nil {
		return StoredReconcilePlan{}, err
	}
	return StoredReconcilePlan{
		Token:     token,
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
		Plan:      plan,
	}, nil
}

func (h Handle) reconcilePlanPath(token string) (string, error) {
	token, err := parseReconcilePlanToken(token)
	if err != nil {
		return "", err
	}
	seriesDir, err := h.files().seriesDir(h.ref)
	if err != nil {
		return "", err
	}
	return filepath.Join(seriesDir.Path(), wire.KuraDir, "reconcile", token+".jsonl"), nil
}

func parseReconcilePlanToken(token string) (string, error) {
	if _, err := ulid.ParseStrict(token); err != nil {
		return "", fmt.Errorf("series: invalid reconcile plan token %q", token)
	}
	return token, nil
}

func toWireReconcilePlan(plan ReconcilePlan) wire.ReconcilePlanV1 {
	out := wire.ReconcilePlanV1{
		Series:    plan.Series.String(),
		FileTitle: plan.FileTitle.String(),
		Snapshot:  plan.Snapshot,
		Changes:   make([]wire.ReconcileChangeV1, 0, len(plan.Changes)),
	}
	for _, change := range plan.Changes {
		out.Changes = append(out.Changes, toWireReconcileChange(change))
	}
	return out
}

func toWireReconcileChange(change Change) wire.ReconcileChangeV1 {
	out := wire.ReconcileChangeV1{
		Kind:       string(change.Kind),
		Episode:    change.Episode.String(),
		From:       change.From,
		To:         change.To,
		Source:     change.Source,
		Resolution: change.Resolution,
		Companions: toWireReconcileMoves(change.Companions),
	}
	if change.Replaced != nil {
		out.Replaced = &wire.ReconcileReplacedV1{
			From:       change.Replaced.From,
			To:         change.Replaced.To,
			Source:     change.Replaced.Source,
			Resolution: change.Replaced.Resolution,
			Companions: toWireReconcileMoves(change.Replaced.Companions),
		}
	}
	return out
}

func toWireReconcileMoves(moves []FileMove) []wire.ReconcileFileMoveV1 {
	if len(moves) == 0 {
		return nil
	}
	out := make([]wire.ReconcileFileMoveV1, 0, len(moves))
	for _, move := range moves {
		out = append(out, wire.ReconcileFileMoveV1{From: move.From, To: move.To})
	}
	return out
}

func fromWireReconcilePlan(in wire.ReconcilePlanV1) (ReconcilePlan, error) {
	seriesRef, err := refs.ParseSeries(in.Series)
	if err != nil {
		return ReconcilePlan{}, err
	}
	out := ReconcilePlan{
		Series:    seriesRef,
		FileTitle: textnorm.NFC(in.FileTitle),
		Snapshot:  in.Snapshot,
		Changes:   make([]Change, 0, len(in.Changes)),
	}
	for _, change := range in.Changes {
		converted, err := fromWireReconcileChange(change)
		if err != nil {
			return ReconcilePlan{}, err
		}
		out.Changes = append(out.Changes, converted)
	}
	return out, nil
}

func fromWireReconcileChange(in wire.ReconcileChangeV1) (Change, error) {
	episode, err := refs.ParseEpisode(in.Episode)
	if err != nil {
		return Change{}, err
	}
	out := Change{
		Kind:       ChangeKind(in.Kind),
		Episode:    episode,
		FileMove:   FileMove{From: in.From, To: in.To},
		Source:     in.Source,
		Resolution: in.Resolution,
		Companions: fromWireReconcileMoves(in.Companions),
	}
	if in.Replaced != nil {
		out.Replaced = &Replaced{
			FileMove:   FileMove{From: in.Replaced.From, To: in.Replaced.To},
			Source:     in.Replaced.Source,
			Resolution: in.Replaced.Resolution,
			Companions: fromWireReconcileMoves(in.Replaced.Companions),
		}
	}
	return out, nil
}

func fromWireReconcileMoves(in []wire.ReconcileFileMoveV1) []FileMove {
	if len(in) == 0 {
		return nil
	}
	out := make([]FileMove, 0, len(in))
	for _, move := range in {
		out = append(out, FileMove{From: move.From, To: move.To})
	}
	return out
}

package series

import (
	"encoding/json"
	"os"
	"time"

	"github.com/wyvernzora/kura/internal/series/wire"
)

type reconcilePlanLog struct {
	file    *os.File
	encoder *json.Encoder
}

func (h Handle) openReconcilePlanLog(token string) (*reconcilePlanLog, error) {
	path, err := h.reconcilePlanPath(token)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, err
	}
	return &reconcilePlanLog{file: file, encoder: json.NewEncoder(file)}, nil
}

func (l *reconcilePlanLog) Close() error {
	return l.file.Close()
}

func (l *reconcilePlanLog) move(at time.Time, phase string, index int, total int, move FileMove, moveErr error) error {
	record := wire.ReconcileEventRecordV1{
		Type:          "event",
		SchemaVersion: wire.CurrentSchemaVersion,
		At:            at.UTC().Format(time.RFC3339),
		Phase:         phase,
		Index:         index,
		Total:         total,
		Move:          wire.ReconcileFileMoveV1{From: move.From, To: move.To},
	}
	if moveErr != nil {
		record.Error = moveErr.Error()
	}
	return l.encoder.Encode(record)
}

func (l *reconcilePlanLog) result(at time.Time, status string, appliedMoves int, applyErr error) error {
	record := wire.ReconcileResultRecordV1{
		Type:          "result",
		SchemaVersion: wire.CurrentSchemaVersion,
		At:            at.UTC().Format(time.RFC3339),
		Status:        status,
		AppliedMoves:  appliedMoves,
	}
	if applyErr != nil {
		record.Error = applyErr.Error()
	}
	return l.encoder.Encode(record)
}

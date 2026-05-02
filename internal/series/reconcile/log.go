package reconcile

import (
	"time"

	"github.com/wyvernzora/kura/internal/storage/planfile"
)

type reconcilePlanLog struct {
	log *planfile.Log
}

func (h Runner) openReconcilePlanLog(token string) (*reconcilePlanLog, error) {
	log, err := planfile.OpenLog(h.root(), h.ref, token)
	if err != nil {
		return nil, err
	}
	return &reconcilePlanLog{log: log}, nil
}

func (l *reconcilePlanLog) Close() error {
	return l.log.Close()
}

func (l *reconcilePlanLog) move(at time.Time, phase string, index int, total int, move FileMove, moveErr error) error {
	return l.log.AppendMove(at, phase, index, total, planfile.FileMove{From: move.From, To: move.To}, moveErr)
}

func (l *reconcilePlanLog) result(at time.Time, status string, appliedMoves int, applyErr error) error {
	return l.log.AppendResult(at, status, appliedMoves, applyErr)
}

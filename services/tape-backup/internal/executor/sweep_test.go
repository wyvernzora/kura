package executor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
)

func TestSweepSnapshotsKeepsEntryWhenEventRecordingFails(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(tapevolume.SnapshotsDir(root), "junk")
	if err := os.MkdirAll(entry, 0o775); err != nil {
		t.Fatalf("MkdirAll snapshot entry: %v", err)
	}
	recordErr := errors.New("journal failed")

	err := sweepSnapshots(root, eventEmitterFunc(func(backupplan.Event) error {
		return recordErr
	}))
	if !errors.Is(err, recordErr) {
		t.Fatalf("sweepSnapshots error = %v, want journal failure", err)
	}
	if _, err := os.Lstat(entry); err != nil {
		t.Fatalf("Lstat snapshot entry after journal failure: %v", err)
	}
}

type eventEmitterFunc func(backupplan.Event) error

func (f eventEmitterFunc) Emit(event backupplan.Event) error {
	return f(event)
}

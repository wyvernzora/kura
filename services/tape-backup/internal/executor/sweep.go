package executor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wyvernzora/kura/services/tape-backup/internal/snapshotname"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
)

const (
	sweepReasonEntryNotDirectory       = "entry_not_directory"
	sweepReasonUnparseableName         = "unparseable_name"
	sweepReasonMissingCompletionMarker = "missing_completion_marker"
	sweepReasonInvalidCompletionMarker = "invalid_completion_marker"
)

type eventEmitter interface {
	Emit(backupplan.Event) error
}

func sweepSnapshots(ltfsRoot string, events eventEmitter) error {
	snapshotsDir := tapevolume.SnapshotsDir(ltfsRoot)
	info, err := os.Lstat(snapshotsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("executor: inspect snapshots directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("executor: snapshots directory must be a real directory")
	}
	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		return fmt.Errorf("executor: read snapshots directory: %w", err)
	}
	for _, entry := range entries {
		if err := sweepEntry(snapshotsDir, entry.Name(), events); err != nil {
			return err
		}
	}
	return nil
}

func sweepEntry(snapshotsDir, name string, events eventEmitter) error {
	entryPath := filepath.Join(snapshotsDir, name)
	info, err := os.Lstat(entryPath)
	if err != nil {
		return fmt.Errorf("executor: inspect snapshot entry %q: %w", name, err)
	}

	reason := ""
	switch {
	case !info.IsDir() || info.Mode()&os.ModeSymlink != 0:
		reason = sweepReasonEntryNotDirectory
	default:
		if _, _, err := snapshotname.Parse(name); err != nil {
			reason = sweepReasonUnparseableName
			break
		}
		markerInfo, err := os.Lstat(filepath.Join(entryPath, "complete.json"))
		switch {
		case errors.Is(err, os.ErrNotExist):
			reason = sweepReasonMissingCompletionMarker
		case err != nil:
			return fmt.Errorf(
				"executor: inspect snapshot %q completion marker: %w",
				name,
				err,
			)
		case !markerInfo.Mode().IsRegular() ||
			markerInfo.Mode()&os.ModeSymlink != 0:
			reason = sweepReasonInvalidCompletionMarker
		}
	}
	if reason == "" {
		return nil
	}
	if err := events.Emit(backupplan.Event{
		Type:   backupplan.EventSnapshotSwept,
		Entry:  name,
		Reason: reason,
	}); err != nil {
		return fmt.Errorf("executor: report swept snapshot %q: %w", name, err)
	}
	if err := os.RemoveAll(entryPath); err != nil {
		return fmt.Errorf("executor: sweep snapshot entry %q: %w", name, err)
	}
	return nil
}

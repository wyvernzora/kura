package workflow

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// RemoveInput parameters for the Remove workflow. Purge=false untracks
// (drops .kura/ only); Purge=true wholesale deletes the series directory.
type RemoveInput struct {
	Ref   refs.Series
	Purge bool
}

// Remove untracks or wholesale deletes a series. Default mode (Purge=false)
// removes <series>/.kura/ and drops the index entry; media files stay in
// place so the directory becomes "untracked." --purge removes the entire
// series directory plus its index entry.
//
// Default mode refuses if the series has staged records — caller must
// reset/reconcile first. Purge mode bypasses the gate (the wholesale
// delete drops staged records along with everything else).
func Remove(ctx context.Context, deps Deps, in RemoveInput) (response.Remove, error) {
	progress.Start(ctx, "remove", fmt.Sprintf("Removing %s", in.Ref), 0)
	if in.Ref.IsZero() {
		progress.Failure(ctx, "remove", "Failed to remove series", 0, 0)
		return response.Remove{}, &NotFoundError{Ref: in.Ref}
	}
	seriesDir := paths.SeriesDir(deps.LibRoot, in.Ref)
	if _, err := os.Stat(seriesDir); err != nil {
		progress.Failure(ctx, "remove", "Failed to remove series", 0, 0)
		if os.IsNotExist(err) {
			return response.Remove{}, &SeriesNotFoundError{Ref: in.Ref}
		}
		return response.Remove{}, err
	}
	// Pre-check series.json: refuse on active claim and (default mode)
	// on staged records. This happens before any mutation.
	if err := preCheckRemove(deps, in); err != nil {
		progress.Failure(ctx, "remove", "Failed to remove series", 0, 0)
		return response.Remove{}, err
	}

	target := paths.SeriesKuraDir(deps.LibRoot, in.Ref)
	if in.Purge {
		target = seriesDir
	}
	bytes, err := dirSize(target)
	if err != nil && !os.IsNotExist(err) {
		progress.Failure(ctx, "remove", "Failed to remove series", 0, 0)
		return response.Remove{}, err
	}

	// Drop the index entry first (CAS); only after success do we touch
	// the filesystem. CAS rejection on the index leaves the series fully
	// tracked exactly as before.
	if err := withIndexCAS(deps, "remove", func(loaded indexfile.Loaded) ([]indexfile.Entry, error) {
		filtered := make([]indexfile.Entry, 0, len(loaded.Entries))
		for _, entry := range loaded.Entries {
			if entry.Series == in.Ref {
				continue
			}
			filtered = append(filtered, entry)
		}
		return filtered, nil
	}); err != nil {
		progress.Failure(ctx, "remove", "Failed to remove series", 0, 0)
		return response.Remove{}, err
	}

	if err := os.RemoveAll(target); err != nil {
		progress.Failure(ctx, "remove", "Failed to remove series", 0, 0)
		return response.Remove{}, err
	}

	progress.Success(ctx, "remove", fmt.Sprintf("Removed %s", in.Ref), 0)
	return response.Remove{ReclaimedBytes: bytes}, nil
}

func preCheckRemove(deps Deps, in RemoveInput) error {
	model, err := seriesfile.Load(deps.LibRoot, in.Ref)
	if err != nil {
		// If series.json is missing or unreadable, the series is already
		// half-broken; let the dir delete proceed (no claim to honor).
		// Other read errors surface.
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if model.InProgress != nil {
		return &coord.BusyError{Scope: coord.SeriesScope(in.Ref), Holder: *model.InProgress}
	}
	if in.Purge {
		return nil
	}
	var staged []refs.Episode
	for ep, episode := range model.Episodes {
		if episode.Staged != nil {
			staged = append(staged, ep)
		}
	}
	if len(staged) == 0 {
		return nil
	}
	sort.Slice(staged, func(i, j int) bool { return staged[i].String() < staged[j].String() })
	return &RemoveStagedRecordsExistError{Ref: in.Ref, Episodes: staged}
}

// dirSize recursively totals file sizes inside dir. Returns 0 with nil
// when dir does not exist.
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return total, nil
}

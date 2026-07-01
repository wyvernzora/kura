package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/internal/storage/trashfile"
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
	model, err := loadRemoveModel(deps, in.Ref)
	if err != nil {
		progress.Failure(ctx, "remove", "Failed to remove series", 0, 0)
		return response.Remove{}, err
	}
	// Pre-check series.json: refuse on active claim and (default mode)
	// on staged records. This happens before any mutation.
	if err := preCheckRemove(model, in); err != nil {
		progress.Failure(ctx, "remove", "Failed to remove series", 0, 0)
		return response.Remove{}, err
	}

	target := paths.SeriesKuraDir(deps.LibRoot, in.Ref)
	if in.Purge {
		target = seriesDir
	}
	bytes := estimatedRemoveBytes(deps.LibRoot, in.Ref, in.Purge, model)

	// Drop the index entry first (CAS); only after success do we touch
	// the filesystem. CAS rejection on the index leaves the series fully
	// tracked exactly as before.
	if err := deps.Index.Delete(ctx, in.Ref, coord.NewMutator("remove")); err != nil {
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

func loadRemoveModel(deps Deps, ref refs.Series) (*domainseries.Series, error) {
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		// If series.json is missing or unreadable, the series is already
		// half-broken; let the dir delete proceed (no claim to honor).
		// Other read errors surface.
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return model, nil
}

func preCheckRemove(model *domainseries.Series, in RemoveInput) error {
	if model == nil {
		return nil
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

// estimatedRemoveBytes is deliberately best-effort. remove --purge may delete
// a huge season tree, so Kura reports the bytes it can learn from known media
// records with direct stat calls instead of walking the directory just for
// accounting.
func estimatedRemoveBytes(root string, ref refs.Series, purge bool, model *domainseries.Series) int64 {
	if model == nil {
		return 0
	}
	var total int64
	if !purge {
		return statFileBytes(paths.SeriesMetadata(root, ref))
	}
	for _, episode := range model.Episodes {
		if episode.Active == nil {
			continue
		}
		total += statFileBytes(episode.Active.Path)
		for _, companion := range episode.Active.Companions {
			total += statFileBytes(companion.Path)
		}
	}
	for _, item := range model.StagedTrash {
		total += statFileBytes(item.Path)
		for _, companion := range item.Companions {
			total += statFileBytes(companion.Path)
		}
	}
	metas, err := trashfile.List(root, ref)
	if err != nil {
		return total
	}
	for _, meta := range metas {
		entryDir := paths.TrashEntry(root, ref, meta.ID.String())
		total += statFileBytes(filepath.Join(entryDir, filepath.Base(meta.Record.Path)))
		for _, companion := range meta.Record.Companions {
			total += statFileBytes(filepath.Join(entryDir, filepath.Base(companion.Path)))
		}
	}
	return total
}

func statFileBytes(path string) int64 {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0
	}
	return info.Size()
}

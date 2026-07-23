package workflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/fsop"
	"github.com/wyvernzora/kura/services/library-manager/internal/progress"
	"github.com/wyvernzora/kura/services/library-manager/internal/response"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/trashfile"
)

// TrashListInput parameters for the TrashList workflow. Exactly one of
// Ref / All must be set.
type TrashListInput struct {
	Ref       refs.Series
	All       bool
	OlderThan time.Duration
	Now       time.Time
}

// TrashEmptyInput parameters for the TrashEmpty workflow. Exactly one
// of Ref / All must be set. Confirm gating is the surface's job.
type TrashEmptyInput struct {
	Ref       refs.Series
	All       bool
	OlderThan time.Duration
	Now       time.Time
}

// TrashRestoreInput parameters for the TrashRestore workflow.
type TrashRestoreInput struct {
	Ref refs.Series
	ID  ulid.ULID
}

// TrashList enumerates trash entries for one series (Ref) or across
// the whole library (All). OlderThan filters to entries trashed at
// least that long before Now (or deps.Now()).
func TrashList(ctx context.Context, deps Deps, in TrashListInput) (response.TrashList, error) {
	progress.Start(ctx, "trash-list", "Scanning trash", 0)
	refsList, err := trashTargetSeries(ctx, deps, in.Ref, in.All)
	if err != nil {
		progress.Failure(ctx, "trash-list", "Failed to list trash", 0, 0)
		return response.TrashList{}, err
	}
	now := in.Now
	if now.IsZero() {
		now = deps.Now()
	}
	out := response.TrashList{Series: make([]response.TrashSeriesEntry, 0, len(refsList))}
	for index, ref := range refsList {
		progress.Update(ctx, "trash-list", fmt.Sprintf("Listing trash for %s", ref), index+1, len(refsList))
		metas, err := trashfile.List(deps.LibRoot, ref)
		if err != nil {
			return response.TrashList{}, err
		}
		seriesRoot := paths.SeriesDir(deps.LibRoot, ref)
		entry := response.TrashSeriesEntry{Ref: ref, Entries: make([]response.TrashEntry, 0, len(metas))}
		for _, meta := range metas {
			if !trashAgePasses(meta.TrashedAt, now, in.OlderThan) {
				continue
			}
			view := trashEntryView(seriesRoot, meta)
			entry.Entries = append(entry.Entries, view)
			entry.Bytes += view.Size
			for _, c := range view.Companions {
				entry.Bytes += c.Size
			}
		}
		if len(entry.Entries) == 0 {
			continue
		}
		out.Series = append(out.Series, entry)
		out.TotalEntries += len(entry.Entries)
		out.TotalBytes += entry.Bytes
	}
	progress.Success(ctx, "trash-list", fmt.Sprintf("Listed trash (%d entries)", out.TotalEntries), len(refsList))
	return out, nil
}

// TrashEmpty deletes trash entries for one series (Ref) or across the
// whole library (All). OlderThan filters to entries trashed at least
// that long before Now (or deps.Now()).
func TrashEmpty(ctx context.Context, deps Deps, in TrashEmptyInput) (response.TrashEmpty, error) {
	progress.Start(ctx, "trash-empty", "Scanning trash", 0)
	refsList, err := trashTargetSeries(ctx, deps, in.Ref, in.All)
	if err != nil {
		progress.Failure(ctx, "trash-empty", "Failed to empty trash", 0, 0)
		return response.TrashEmpty{}, err
	}
	now := in.Now
	if now.IsZero() {
		now = deps.Now()
	}
	out := response.TrashEmpty{Series: make([]response.TrashSeriesEmpty, 0, len(refsList))}
	for index, ref := range refsList {
		progress.Update(ctx, "trash-empty", fmt.Sprintf("Emptying trash for %s", ref), index+1, len(refsList))
		series := response.TrashSeriesEmpty{Ref: ref, Removed: make([]string, 0)}
		// attempts counts entries the inner closure actually tried to
		// delete (matched OlderThan), so callers can distinguish "no
		// trash for this series" from "every attempt failed."
		attempts := 0
		emptyErr := deps.Coordinator.WithSeries(ctx, ref, func() error {
			if err := refuseIfClaimed(deps, ref); err != nil {
				return err
			}
			metas, err := trashfile.List(deps.LibRoot, ref)
			if err != nil {
				return err
			}
			// Per-bucket failures are collected and joined at the end
			// of the series. Earlier code aborted at the first failed
			// bucket which masked how many buckets were affected and
			// stranded the rest of the trash for that series until a
			// subsequent retry. The joined error preserves every
			// bucket's wrapped cause for the slog Warn line and the
			// CLI Failures envelope.
			var bucketErrs []error
			for _, meta := range metas {
				if !trashAgePasses(meta.TrashedAt, now, in.OlderThan) {
					continue
				}
				attempts++
				bytes, err := trashfile.Delete(deps.LibRoot, ref, meta.ID)
				if err != nil {
					bucketErrs = append(bucketErrs, fmt.Errorf("workflow: trash empty %s/%s: %w", ref, meta.ID, err))
					continue
				}
				series.Removed = append(series.Removed, meta.ID.String())
				series.ReclaimedBytes += bytes
			}
			if len(bucketErrs) > 0 {
				return errors.Join(bucketErrs...)
			}
			return nil
		})
		out.Attempts += attempts
		if emptyErr != nil {
			if deps.Logger != nil {
				deps.Logger.Warn("trash empty per-series failure",
					"ref", ref.String(),
					"attempts", attempts,
					"removed", len(series.Removed),
					"err", emptyErr.Error(),
				)
			}
			out.Failures = append(out.Failures, response.TrashEmptyFailure{
				Ref:   ref,
				Error: emptyErr.Error(),
			})
			if in.All {
				// Best-effort under --all: continue to subsequent
				// series. The per-series failure was already logged
				// + appended to out.Failures so the caller sees it.
				if len(series.Removed) > 0 {
					out.Series = append(out.Series, series)
					out.TotalEntries += len(series.Removed)
					out.ReclaimedBytes += series.ReclaimedBytes
				}
				continue
			}
			return response.TrashEmpty{}, emptyErr
		}
		if len(series.Removed) == 0 {
			continue
		}
		out.Series = append(out.Series, series)
		out.TotalEntries += len(series.Removed)
		out.ReclaimedBytes += series.ReclaimedBytes
	}
	progress.Success(ctx, "trash-empty", fmt.Sprintf("Emptied trash (%d entries)", out.TotalEntries), len(refsList))
	return out, nil
}

// TrashRestore moves files from a trash entry back to their recorded
// paths. Refuses if any target path already exists OR if a reconcile
// apply (or any other claim-holder) is mid-flight on the series.
// Filesystem-only; caller runs scan afterward to re-adopt the files
// into series.json.
func TrashRestore(ctx context.Context, deps Deps, in TrashRestoreInput) (response.TrashRestore, error) {
	var out response.TrashRestore
	err := deps.Coordinator.WithSeries(ctx, in.Ref, func() error {
		if err := refuseIfClaimed(deps, in.Ref); err != nil {
			return err
		}
		result, runErr := trashRestoreLocked(deps, in)
		if runErr != nil {
			return runErr
		}
		out = result
		return nil
	})
	return out, err
}

func trashRestoreLocked(deps Deps, in TrashRestoreInput) (response.TrashRestore, error) {
	meta, err := trashfile.Read(deps.LibRoot, in.Ref, in.ID)
	if err != nil {
		return response.TrashRestore{}, err
	}
	seriesRoot := paths.SeriesDir(deps.LibRoot, in.Ref)
	type plannedMove struct {
		from string
		to   string
	}
	moves := make([]plannedMove, 0, 1+len(meta.Record.Companions))
	moves = append(moves, plannedMove{
		from: filepath.Join(paths.TrashEntry(deps.LibRoot, in.Ref, in.ID.String()), filepath.Base(meta.Record.Path)),
		to:   filepath.Join(seriesRoot, filepath.FromSlash(meta.Record.Path)),
	})
	for _, companion := range meta.Record.Companions {
		moves = append(moves, plannedMove{
			from: filepath.Join(paths.TrashEntry(deps.LibRoot, in.Ref, in.ID.String()), filepath.Base(companion.Path)),
			to:   filepath.Join(seriesRoot, filepath.FromSlash(companion.Path)),
		})
	}
	var conflicts []string
	for _, move := range moves {
		if _, err := os.Stat(move.to); err == nil {
			conflicts = append(conflicts, move.to)
		} else if !errors.Is(err, os.ErrNotExist) {
			return response.TrashRestore{}, err
		}
	}
	if len(conflicts) > 0 {
		return response.TrashRestore{}, &TrashRestoreTargetExistsError{Ref: in.Ref, ID: in.ID.String(), Targets: conflicts}
	}
	restored := make([]string, 0, len(moves))
	for _, move := range moves {
		if err := fsop.SafeMoveFile(move.from, move.to); err != nil {
			return response.TrashRestore{}, fmt.Errorf("workflow: trash restore move %q -> %q: %w", move.from, move.to, err)
		}
		logFileMove(deps, "trash_restore",
			"ref", in.Ref.String(),
			"id", in.ID.String(),
			"from", move.from,
			"to", move.to,
		)
		restored = append(restored, seriesSelector(seriesRoot, move.to))
	}
	if _, err := trashfile.Delete(deps.LibRoot, in.Ref, in.ID); err != nil {
		return response.TrashRestore{}, fmt.Errorf("workflow: trash restore cleanup %s: %w", in.ID, err)
	}
	return response.TrashRestore{
		Episode:  meta.Episode,
		Restored: restored,
	}, nil
}

// refuseIfClaimed loads the series.json (if present) and returns a
// BusyError when an in_progress claim is set. Missing series.json is
// not an error — it means there's no claim to honor.
func refuseIfClaimed(deps Deps, ref refs.Series) error {
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if model.InProgress != nil {
		return &coord.BusyError{Scope: coord.SeriesScope(ref), Holder: *model.InProgress}
	}
	return nil
}

func trashTargetSeries(ctx context.Context, deps Deps, ref refs.Series, all bool) ([]refs.Series, error) {
	if all && !ref.IsZero() {
		return nil, errors.New("workflow: trash invocation cannot pass both Ref and All")
	}
	if !all && ref.IsZero() {
		return nil, errors.New("workflow: trash invocation requires Ref or All")
	}
	if !all {
		return []refs.Series{ref}, nil
	}
	dir, err := os.Open(deps.LibRoot)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	var out []refs.Series
	scanned := 0
	for {
		entries, readErr := dir.ReadDir(64)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() || strings.HasPrefix(name, ".") {
				continue
			}
			scanned++
			progress.Update(ctx, "trash-walk", fmt.Sprintf("Scanning %s", name), scanned, 0)
			parsed, err := refs.ParseSeries(name)
			if err != nil {
				continue
			}
			out = append(out, parsed)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out, nil
}

func trashAgePasses(trashedAt, now time.Time, olderThan time.Duration) bool {
	if olderThan <= 0 {
		return true
	}
	return now.Sub(trashedAt) >= olderThan
}

func trashEntryView(seriesRoot string, meta trashfile.Meta) response.TrashEntry {
	view := response.TrashEntry{
		ID:         meta.ID.String(),
		Episode:    meta.Episode,
		TrashedAt:  meta.TrashedAt,
		MediaPath:  seriesSelector(seriesRoot, meta.Record.Path),
		Source:     meta.Record.Source,
		Resolution: meta.Record.Resolution,
		Size:       meta.Record.Size,
	}
	if len(meta.Record.Companions) > 0 {
		view.Companions = make([]response.TrashCompanion, 0, len(meta.Record.Companions))
		for _, c := range meta.Record.Companions {
			view.Companions = append(view.Companions, response.TrashCompanion{
				Path: seriesSelector(seriesRoot, c.Path),
				Size: c.Size,
			})
		}
	}
	return view
}

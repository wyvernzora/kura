package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/reconcile"
	"github.com/wyvernzora/kura/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/fsop"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/planfile"
	"github.com/wyvernzora/kura/internal/storage/seriesdir"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/internal/storage/trashfile"
)

// ApplyReconcileInput parameters for the ApplyReconcile workflow.
type ApplyReconcileInput struct {
	Ref   refs.Series
	Token string
}

// ApplyReconcile loads the persisted plan, validates it against the current
// series state, acquires the in_progress claim, executes the moves, appends
// per-move events to the plan log, and updates series.json on success.
//
// Returns a tracked *jobs.Job; CLI callers Wait for the typed result, MCP
// long-tool callers hand the ID off to a polling client. Wrapped in
// coord.WithSeries (no retry) since the side effects (file moves, trash
// population) are not safely re-runnable. Conflicts surface as
// BusyError / ReconcileInProgressError; the caller decides what to do.
func ApplyReconcile(ctx context.Context, deps Deps, in ApplyReconcileInput) *jobs.Job[response.ReconcileApply] {
	return jobs.Submit(deps.Jobs, jobs.KindReconcileApply, in.Ref, func(jobCtx context.Context) (response.ReconcileApply, error) {
		var out response.ReconcileApply
		err := deps.Coordinator.WithSeries(in.Ref, func() error {
			result, runErr := applyReconcileLocked(jobCtx, deps, in)
			if runErr != nil {
				return runErr
			}
			out = result
			return nil
		})
		return out, err
	})
}

func applyReconcileLocked(ctx context.Context, deps Deps, in ApplyReconcileInput) (response.ReconcileApply, error) {
	record, applied, err := planfile.ReadPlan(deps.LibRoot, in.Ref, in.Token)
	if err != nil {
		return response.ReconcileApply{}, err
	}
	if applied {
		return response.ReconcileApply{}, &ReconcilePlanAlreadyAppliedError{Token: in.Token}
	}
	if record.Plan.Series != in.Ref {
		return response.ReconcileApply{}, reconcile.StaleSnapshotError{Series: record.Plan.Series}
	}
	log, err := planfile.OpenLog(deps.LibRoot, in.Ref, in.Token)
	if err != nil {
		return response.ReconcileApply{}, err
	}
	defer log.Close()
	if deps.Now().UTC().After(record.ExpiresAt) {
		expiredErr := &ReconcilePlanExpiredError{Token: in.Token, ExpiresAt: record.ExpiresAt}
		_ = log.AppendResult(deps.Now(), "failure", 0, expiredErr)
		return response.ReconcileApply{}, expiredErr
	}
	return executeReconcile(ctx, deps, in.Ref, in.Token, record.Plan, log)
}

func executeReconcile(ctx context.Context, deps Deps, ref refs.Series, token string, plan reconcile.Plan, log *planfile.Log) (response.ReconcileApply, error) {
	progress.Start(ctx, "reconcile", fmt.Sprintf("Applying reconcile for %s", ref), 0)
	if err := validateAppliedPlan(deps.LibRoot, ref, plan); err != nil {
		_ = log.AppendResult(deps.Now(), "failure", 0, err)
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), 0, 0)
		return response.ReconcileApply{}, err
	}
	if !plan.HasChanges() {
		if err := log.AppendResult(deps.Now(), "success", 0, nil); err != nil {
			progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), 0, 0)
			return response.ReconcileApply{}, err
		}
		progress.Success(ctx, "reconcile", fmt.Sprintf("Reconciled %s", ref), 0)
		return response.ReconcileApply{Series: ref}, nil
	}
	seriesDir, err := seriesdir.Parse(paths.SeriesDir(deps.LibRoot, ref))
	if err != nil {
		_ = log.AppendResult(deps.Now(), "failure", 0, err)
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), 0, 0)
		return response.ReconcileApply{}, err
	}

	// Acquire the in_progress claim. Loaded carries the hash for the
	// final CAS write at the end. preLoaded captures pre-claim state
	// for trash meta.json (active records).
	preLoaded, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		_ = log.AppendResult(deps.Now(), "failure", 0, err)
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), 0, 0)
		return response.ReconcileApply{}, err
	}
	holder, claimErr := acquireReconcileClaim(deps, preLoaded, token)
	if claimErr != nil {
		_ = log.AppendResult(deps.Now(), "failure", 0, claimErr)
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), 0, 0)
		return response.ReconcileApply{}, claimErr
	}
	claimedHash := preLoaded.Hash

	// Defer claim release: best-effort clear on any error path. The
	// release uses CAS expecting our own hash; if a peer broke our
	// claim and updated the file (cross-host stale break, manual
	// recovery, etc.) the release silently fails — that's the right
	// outcome (don't undo someone else's recovery).
	released := false
	defer func() {
		if released {
			return
		}
		_ = releaseReconcileClaim(deps, ref, holder, claimedHash)
	}()

	// Write trash meta.json for every replace before any file moves run.
	// A crash mid-move then leaves a self-describing trash entry rather
	// than orphan files in a ULID dir without metadata.
	if err := writeAllReconcileTrash(deps, ref, plan, preLoaded); err != nil {
		_ = log.AppendResult(deps.Now(), "failure", 0, err)
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), 0, 0)
		return response.ReconcileApply{}, err
	}
	moves := plan.Moves()
	for index, move := range moves {
		progress.Update(ctx, "reconcile", fmt.Sprintf("Moving %s", filepath.Base(move.To)), index+1, len(moves))
		from := move.From
		if !filepath.IsAbs(from) {
			from = filepath.Join(seriesDir.Path(), filepath.FromSlash(move.From))
		}
		to := filepath.Join(seriesDir.Path(), filepath.FromSlash(move.To))
		moveErr := fsop.SafeMoveFile(from, to)
		if err := log.AppendMove(deps.Now(), index+1, len(moves), move, moveErr); err != nil {
			progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), index+1, len(moves))
			return response.ReconcileApply{}, err
		}
		if moveErr != nil {
			_ = log.AppendResult(deps.Now(), "failure", index, moveErr)
			progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), index+1, len(moves))
			return response.ReconcileApply{}, moveErr
		}
	}

	// Reload to verify our claim is still ours, then compute the
	// post-apply state, clear the claim, and CAS-write.
	postLoaded, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		_ = log.AppendResult(deps.Now(), "failure", len(moves), err)
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), len(moves), len(moves))
		return response.ReconcileApply{}, err
	}
	if !claimMatches(postLoaded.InProgress, holder) {
		stolen := &coord.ClaimStolenError{Scope: coord.SeriesScope(ref), Expected: holder, Found: postLoaded.InProgress}
		_ = log.AppendResult(deps.Now(), "failure", len(moves), stolen)
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), len(moves), len(moves))
		return response.ReconcileApply{}, stolen
	}
	updated, err := computePostApplyState(postLoaded, plan)
	if err != nil {
		_ = log.AppendResult(deps.Now(), "failure", len(moves), err)
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), len(moves), len(moves))
		return response.ReconcileApply{}, err
	}
	updated.InProgress = nil
	if err := seriesfile.SaveCAS(deps.LibRoot, updated, coord.NewMutator("reconcile_apply")); err != nil {
		_ = log.AppendResult(deps.Now(), "failure", len(moves), err)
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), len(moves), len(moves))
		return response.ReconcileApply{}, err
	}
	released = true
	if err := log.AppendResult(deps.Now(), "success", len(moves), nil); err != nil {
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), len(moves), len(moves))
		return response.ReconcileApply{}, err
	}
	progress.Success(ctx, "reconcile", fmt.Sprintf("Reconciled %s", ref), len(moves))
	return response.ReconcileApply{Series: ref, AppliedMoves: len(moves)}, nil
}

// acquireReconcileClaim sets in_progress on the series and CAS-writes.
// Surfaces ReconcileInProgressError for live same-token holders,
// BusyError for live cross-token / cross-host holders, and silently
// breaks stale same-host claims.
func acquireReconcileClaim(deps Deps, loaded *domainseries.Series, token string) (coord.Holder, error) {
	if existing := loaded.InProgress; existing != nil {
		if !coord.IsStaleHolder(*existing) {
			if existing.Op == "reconcile_apply" && existing.Token == token {
				return coord.Holder{}, &ReconcileInProgressError{Token: token, Holder: *existing}
			}
			return coord.Holder{}, &coord.BusyError{Scope: coord.SeriesScope(loaded.Ref), Holder: *existing}
		}
		// Stale; fall through to overwrite.
	}
	holder := coord.NewHolder("reconcile_apply", token)
	loaded.InProgress = &holder
	if err := seriesfile.SaveCAS(deps.LibRoot, loaded, coord.NewMutator("reconcile_apply_claim")); err != nil {
		return coord.Holder{}, err
	}
	return holder, nil
}

func releaseReconcileClaim(deps Deps, ref refs.Series, holder coord.Holder, expectedHash string) error {
	loaded, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		return err
	}
	if !claimMatches(loaded.InProgress, holder) {
		return nil
	}
	loaded.InProgress = nil
	// Use the hash we just loaded; if a peer races we surface the
	// conflict to the caller (which is logging best-effort already).
	_ = expectedHash
	return seriesfile.SaveCAS(deps.LibRoot, loaded, coord.NewMutator("reconcile_apply_release"))
}

func claimMatches(found *coord.Holder, want coord.Holder) bool {
	if found == nil {
		return false
	}
	return found.PID == want.PID && found.Host == want.Host && found.Started.Equal(want.Started) && found.Token == want.Token
}

// computePostApplyState mutates loaded in place to reflect the plan's
// changes (path promotions, staged → active transitions). Returns the
// same pointer for clarity.
func computePostApplyState(loaded *domainseries.Series, plan reconcile.Plan) (*domainseries.Series, error) {
	for _, change := range plan.Changes {
		episode := loaded.Episodes[change.Episode]
		switch change.Kind {
		case reconcile.ChangeAdd, reconcile.ChangeReplace:
			if episode.Staged == nil {
				return nil, fmt.Errorf("workflow: %s has no staged media", change.Episode)
			}
			episode.Staged.Path = change.To
			for index := range episode.Staged.Companions {
				if index < len(change.Companions) {
					episode.Staged.Companions[index].Path = change.Companions[index].To
				}
			}
			loaded.Episodes[change.Episode] = episode
			if _, err := loaded.PromoteStaged(change.Episode); err != nil {
				return nil, err
			}
		case reconcile.ChangeMove:
			if episode.Active == nil {
				return nil, fmt.Errorf("workflow: %s has no active media", change.Episode)
			}
			episode.Active.Path = change.To
			for index := range episode.Active.Companions {
				if index < len(change.Companions) {
					episode.Active.Companions[index].Path = change.Companions[index].To
				}
			}
			loaded.Episodes[change.Episode] = episode
		default:
			return nil, fmt.Errorf("workflow: unsupported reconcile change kind %q", change.Kind)
		}
	}
	return loaded, nil
}

func validateAppliedPlan(root string, ref refs.Series, plan reconcile.Plan) error {
	data, err := os.ReadFile(paths.SeriesMetadata(root, ref))
	if err != nil {
		return err
	}
	return reconcile.ValidateSnapshot(plan, data)
}

// writeAllReconcileTrash writes meta.json for every Replaced change in
// the plan before any file move runs. Idempotent across retries: the
// underlying renameio.WriteFile overwrites whatever was there.
func writeAllReconcileTrash(deps Deps, ref refs.Series, plan reconcile.Plan, series *domainseries.Series) error {
	for _, change := range plan.Changes {
		if change.Replaced == nil {
			continue
		}
		episode, ok := series.Episodes[change.Episode]
		if !ok || episode.Active == nil {
			continue
		}
		if err := writeReconcileTrash(deps, ref, change.Episode, *episode.Active, *change.Replaced); err != nil {
			return fmt.Errorf("workflow: pre-write trash for %s: %w", change.Episode, err)
		}
	}
	return nil
}

func writeReconcileTrash(deps Deps, ref refs.Series, episode refs.Episode, record media.Record, replaced reconcile.Replaced) error {
	id, err := trashIDFromPath(replaced.To)
	if err != nil {
		return err
	}
	record.Path = replaced.To
	for index := range record.Companions {
		if index < len(replaced.Companions) {
			record.Companions[index].Path = replaced.Companions[index].To
		}
	}
	return trashfile.Write(deps.LibRoot, ref, trashfile.Meta{
		ID:        id,
		Episode:   episode,
		TrashedAt: deps.Now().UTC(),
		Record:    trashRecordFromMedia(record),
	})
}

func trashRecordFromMedia(in media.Record) trashfile.Record {
	out := trashfile.Record{
		Path:       in.Path,
		Source:     in.Source.String(),
		Resolution: in.Resolution.String(),
		Codec:      in.Codec.String(),
		Size:       in.Size,
		MTime:      in.MTime,
		Companions: make([]trashfile.Companion, 0, len(in.Companions)),
	}
	for _, companion := range in.Companions {
		out.Companions = append(out.Companions, trashfile.Companion{
			Path:     companion.Path,
			Role:     companion.Role,
			Language: companion.Language,
			Label:    companion.Label,
			Size:     companion.Size,
			MTime:    companion.MTime,
		})
	}
	return out
}

func trashIDFromPath(path string) (ulid.ULID, error) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 3 {
		return ulid.ULID{}, fmt.Errorf("workflow: trash path %q missing ulid", path)
	}
	return ulid.Parse(parts[2])
}

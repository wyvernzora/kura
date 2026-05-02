package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/reconcile"
	"github.com/wyvernzora/kura/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/fsop"
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
// series state, executes the moves, appends per-move events to the plan log,
// and updates series.json on success.
func ApplyReconcile(ctx context.Context, deps Deps, in ApplyReconcileInput) (response.ReconcileApply, error) {
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
	return executeReconcile(ctx, deps, in.Ref, record.Plan, log)
}

func executeReconcile(ctx context.Context, deps Deps, ref refs.Series, plan reconcile.Plan, log *planfile.Log) (response.ReconcileApply, error) {
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
	updated, err := applyPlanToSeries(deps, ref, plan)
	if err != nil {
		_ = log.AppendResult(deps.Now(), "failure", len(moves), err)
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), len(moves), len(moves))
		return response.ReconcileApply{}, err
	}
	if err := seriesfile.Save(deps.LibRoot, updated); err != nil {
		_ = log.AppendResult(deps.Now(), "failure", len(moves), err)
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), len(moves), len(moves))
		return response.ReconcileApply{}, err
	}
	if err := log.AppendResult(deps.Now(), "success", len(moves), nil); err != nil {
		progress.Failure(ctx, "reconcile", fmt.Sprintf("Failed to reconcile %s", ref), len(moves), len(moves))
		return response.ReconcileApply{}, err
	}
	progress.Success(ctx, "reconcile", fmt.Sprintf("Reconciled %s", ref), len(moves))
	return response.ReconcileApply{Series: ref, AppliedMoves: len(moves)}, nil
}

func validateAppliedPlan(root string, ref refs.Series, plan reconcile.Plan) error {
	data, err := os.ReadFile(paths.SeriesMetadata(root, ref))
	if err != nil {
		return err
	}
	return reconcile.ValidateSnapshot(plan, data)
}

func applyPlanToSeries(deps Deps, ref refs.Series, plan reconcile.Plan) (*domainseries.Series, error) {
	series, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		return nil, err
	}
	for _, change := range plan.Changes {
		episode := series.Episodes[change.Episode]
		switch change.Kind {
		case reconcile.ChangeAdd, reconcile.ChangeReplace:
			if episode.Staged == nil {
				return nil, fmt.Errorf("workflow: %s has no staged media", change.Episode)
			}
			if change.Replaced != nil && episode.Active != nil {
				if err := writeReconcileTrash(deps, ref, change.Episode, *episode.Active, *change.Replaced); err != nil {
					return nil, err
				}
			}
			episode.Staged.Path = change.To
			for index := range episode.Staged.Companions {
				if index < len(change.Companions) {
					episode.Staged.Companions[index].Path = change.Companions[index].To
				}
			}
			series.Episodes[change.Episode] = episode
			if _, err := series.PromoteStaged(change.Episode); err != nil {
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
			series.Episodes[change.Episode] = episode
		default:
			return nil, fmt.Errorf("workflow: unsupported reconcile change kind %q", change.Kind)
		}
	}
	return series, nil
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

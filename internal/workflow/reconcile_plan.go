package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/reconcile"
	"github.com/wyvernzora/kura/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/series/layout"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/planfile"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// PlanReconcileTTL is how long a persisted plan is valid for apply.
const PlanReconcileTTL = 5 * time.Minute

// PlanReconcileInput parameters for the PlanReconcile workflow.
type PlanReconcileInput struct {
	Ref refs.Series
}

// PlanReconcile loads the series state, computes the change set against the
// canonical layout, and persists a plan record under
// <series>/.kura/reconcile/<token>.jsonl when there is work to do. Empty
// plans return without writing.
func PlanReconcile(ctx context.Context, deps Deps, in PlanReconcileInput) (response.ReconcilePlan, error) {
	_ = ctx
	series, err := seriesfile.Load(deps.LibRoot, in.Ref)
	if err != nil {
		return response.ReconcilePlan{}, err
	}
	rawSeries, err := os.ReadFile(paths.SeriesMetadata(deps.LibRoot, in.Ref))
	if err != nil {
		return response.ReconcilePlan{}, err
	}
	seriesDir, err := layout.NewFiles(deps.LibRoot).SeriesDir(in.Ref)
	if err != nil {
		return response.ReconcilePlan{}, err
	}
	changes, err := computeReconcileChanges(deps.LibRoot, in.Ref, series, seriesDir.Path())
	if err != nil {
		return response.ReconcilePlan{}, err
	}
	if err := validateReconcileMoves(seriesDir.Path(), changes); err != nil {
		return response.ReconcilePlan{}, err
	}
	plan := reconcile.Plan{
		Series:   in.Ref,
		Snapshot: reconcile.Snapshot(rawSeries),
		Changes:  changes,
	}
	out := response.ReconcilePlan{Plan: planToResponse(plan)}
	if !plan.HasChanges() {
		return out, nil
	}
	token := ulid.Make().String()
	createdAt := deps.Now().UTC()
	expiresAt := createdAt.Add(PlanReconcileTTL)
	record := planfile.PlanRecord{
		Token:     token,
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
		Plan:      plan,
	}
	if err := planfile.WritePlan(deps.LibRoot, in.Ref, record); err != nil {
		return response.ReconcilePlan{}, err
	}
	out.Token = token
	out.CreatedAt = &createdAt
	out.ExpiresAt = &expiresAt
	return out, nil
}

func computeReconcileChanges(root string, ref refs.Series, series *domainseries.Series, seriesDirPath string) ([]reconcile.Change, error) {
	episodeRefs := make([]refs.Episode, 0, len(series.Episodes))
	for episodeRef := range series.Episodes {
		episodeRefs = append(episodeRefs, episodeRef)
	}
	sort.Slice(episodeRefs, func(i, j int) bool { return episodeRefs[i].String() < episodeRefs[j].String() })
	files := layout.NewFiles(root)
	changes := make([]reconcile.Change, 0)
	for _, episodeRef := range episodeRefs {
		episode := series.Episodes[episodeRef]
		if episode.Staged != nil {
			change, err := stagedReconcileChange(files, ref, seriesDirPath, episodeRef, episode)
			if err != nil {
				return nil, err
			}
			changes = append(changes, change)
			continue
		}
		if episode.Active != nil {
			change, ok, err := moveReconcileChange(files, ref, seriesDirPath, episodeRef, *episode.Active)
			if err != nil {
				return nil, err
			}
			if ok {
				changes = append(changes, change)
			}
		}
	}
	return changes, nil
}

func stagedReconcileChange(files layout.Files, ref refs.Series, seriesDirPath string, episodeRef refs.Episode, episode domainseries.Episode) (reconcile.Change, error) {
	target, err := files.CanonicalPath(ref, episodeRef, *episode.Staged)
	if err != nil {
		return reconcile.Change{}, err
	}
	change := reconcile.Change{
		Kind:       reconcile.ChangeAdd,
		Episode:    episodeRef,
		FileMove:   reconcile.FileMove{From: episode.Staged.Path, To: target},
		Source:     episode.Staged.Source.String(),
		Resolution: episode.Staged.Resolution.String(),
		Companions: companionReconcileMoves(episode.Staged.Path, target, episode.Staged.Companions),
	}
	if episode.Active != nil {
		// Self-refresh: staged points at the same physical file as active
		// (typically a metadata-only update such as changing Source). Skip
		// the trash step entirely; the move loop renames in place if the
		// canonical filename changed, and applyPlanToSeries promotes the
		// staged record over active.
		if episode.Active.Path == episode.Staged.Path {
			return change, nil
		}
		activeRel, err := relativeRecord(seriesDirPath, *episode.Active)
		if err != nil {
			return reconcile.Change{}, err
		}
		id := ulid.Make()
		change.Kind = reconcile.ChangeReplace
		change.Replaced = &reconcile.Replaced{
			FileMove:   reconcile.FileMove{From: activeRel.Path, To: trashRelPath(id, activeRel.Path)},
			Source:     activeRel.Source.String(),
			Resolution: activeRel.Resolution.String(),
			Companions: trashCompanionMoves(id, activeRel.Companions),
		}
	}
	return change, nil
}

func moveReconcileChange(files layout.Files, ref refs.Series, seriesDirPath string, episodeRef refs.Episode, active media.Record) (reconcile.Change, bool, error) {
	active, err := relativeRecord(seriesDirPath, active)
	if err != nil {
		return reconcile.Change{}, false, err
	}
	target, err := files.CanonicalPath(ref, episodeRef, active)
	if err != nil {
		return reconcile.Change{}, false, err
	}
	companions := companionReconcileMoves(active.Path, target, active.Companions)
	if target == active.Path && len(companions) == 0 {
		return reconcile.Change{}, false, nil
	}
	return reconcile.Change{
		Kind:       reconcile.ChangeMove,
		Episode:    episodeRef,
		FileMove:   reconcile.FileMove{From: active.Path, To: target},
		Source:     active.Source.String(),
		Resolution: active.Resolution.String(),
		Companions: companions,
	}, true, nil
}

// relativeRecord returns a copy of an active record with paths rewritten
// relative to the series directory. seriesfile.Load absolutizes active paths
// in memory; reconcile compares against canonical paths (relative) and
// persists FileMove records with relative paths, so it converts back at the
// boundary.
func relativeRecord(seriesDirPath string, record media.Record) (media.Record, error) {
	out := media.CloneRecord(record)
	if filepath.IsAbs(out.Path) {
		rel, err := filepath.Rel(seriesDirPath, out.Path)
		if err != nil {
			return media.Record{}, err
		}
		out.Path = filepath.ToSlash(rel)
	}
	for i := range out.Companions {
		if filepath.IsAbs(out.Companions[i].Path) {
			rel, err := filepath.Rel(seriesDirPath, out.Companions[i].Path)
			if err != nil {
				return media.Record{}, err
			}
			out.Companions[i].Path = filepath.ToSlash(rel)
		}
	}
	return out, nil
}

func companionReconcileMoves(oldMediaPath, newMediaPath string, companions []media.Companion) []reconcile.FileMove {
	oldBase := strings.TrimSuffix(filepath.Base(oldMediaPath), filepath.Ext(oldMediaPath))
	newBase := strings.TrimSuffix(filepath.Base(newMediaPath), filepath.Ext(newMediaPath))
	dir := filepath.Dir(newMediaPath)
	if dir == "." {
		dir = ""
	}
	moves := make([]reconcile.FileMove, 0, len(companions))
	for _, companion := range companions {
		target := filepath.ToSlash(filepath.Join(dir, newBase+companionSuffix(filepath.Base(companion.Path), oldBase)))
		if target != companion.Path {
			moves = append(moves, reconcile.FileMove{From: companion.Path, To: target})
		}
	}
	return moves
}

func companionSuffix(filename, oldMediaBase string) string {
	if strings.HasPrefix(filename, oldMediaBase+".") {
		return strings.TrimPrefix(filename, oldMediaBase)
	}
	extension := compoundExtension(filename)
	if extension == "" {
		return filepath.Ext(filename)
	}
	return extension
}

func compoundExtension(filename string) string {
	name := filepath.Base(filename)
	index := strings.Index(name, ".")
	if index < 0 {
		return ""
	}
	return name[index:]
}

func trashRelPath(id ulid.ULID, path string) string {
	return paths.TrashRel(id.String(), filepath.Base(path))
}

func trashCompanionMoves(id ulid.ULID, companions []media.Companion) []reconcile.FileMove {
	moves := make([]reconcile.FileMove, 0, len(companions))
	for _, companion := range companions {
		moves = append(moves, reconcile.FileMove{From: companion.Path, To: trashRelPath(id, companion.Path)})
	}
	return moves
}

func validateReconcileMoves(seriesDirPath string, changes []reconcile.Change) error {
	targets := map[string]string{}
	relativeSources := map[string]struct{}{}
	for _, change := range changes {
		for _, move := range change.Moves() {
			if move.From == move.To {
				continue
			}
			if existing, exists := targets[move.To]; exists && existing != move.From {
				return fmt.Errorf("multiple tracked files target %q", move.To)
			}
			targets[move.To] = move.From
			if !filepath.IsAbs(move.From) {
				relativeSources[move.From] = struct{}{}
			}
		}
	}
	for target, source := range targets {
		targetAbs := filepath.Join(seriesDirPath, filepath.FromSlash(target))
		sourceAbs := source
		if !filepath.IsAbs(sourceAbs) {
			sourceAbs = filepath.Join(seriesDirPath, filepath.FromSlash(source))
		}
		if _, err := os.Stat(targetAbs); err == nil && targetAbs != sourceAbs {
			if _, movedAway := relativeSources[target]; movedAway {
				continue
			}
			return fmt.Errorf("target path %q already exists", target)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func planToResponse(plan reconcile.Plan) response.ReconcilePlanDetail {
	out := response.ReconcilePlanDetail{
		Series:  plan.Series,
		Changes: make([]response.ReconcileChange, 0, len(plan.Changes)),
	}
	for _, change := range plan.Changes {
		entry := response.ReconcileChange{
			Kind:       string(change.Kind),
			Episode:    change.Episode,
			From:       change.From,
			To:         change.To,
			Source:     change.Source,
			Resolution: change.Resolution,
			Companions: movesToResponse(change.Companions),
		}
		if change.Replaced != nil {
			entry.Replaced = &response.ReconcileReplaced{
				From:       change.Replaced.From,
				To:         change.Replaced.To,
				Source:     change.Replaced.Source,
				Resolution: change.Replaced.Resolution,
				Companions: movesToResponse(change.Replaced.Companions),
			}
		}
		out.Changes = append(out.Changes, entry)
	}
	return out
}

func movesToResponse(in []reconcile.FileMove) []response.ReconcileMove {
	if len(in) == 0 {
		return nil
	}
	out := make([]response.ReconcileMove, 0, len(in))
	for _, m := range in {
		out = append(out, response.ReconcileMove{From: m.From, To: m.To})
	}
	return out
}

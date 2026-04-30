package series

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/trash"
)

type ReconcilePlan struct {
	Series    refs.Series
	FileTitle string
	Snapshot  string
	Changes   []Change
}

func (p ReconcilePlan) HasChanges() bool {
	return len(p.Changes) > 0
}

type FileMove struct {
	From string
	To   string
}

type Change struct {
	Kind    ChangeKind
	Episode refs.Episode
	FileMove
	Source     string
	Resolution string
	Companions []FileMove
	Replaced   *Replaced
}

func (c Change) Moves() []FileMove {
	moves := make([]FileMove, 0, 2+len(c.Companions))
	if c.Replaced != nil {
		moves = append(moves, c.Replaced.FileMove)
		moves = append(moves, c.Replaced.Companions...)
	}
	moves = append(moves, c.FileMove)
	moves = append(moves, c.Companions...)
	return moves
}

type ChangeKind string

const (
	ChangeAdd     ChangeKind = "add"
	ChangeMove    ChangeKind = "move"
	ChangeReplace ChangeKind = "replace"
)

type Replaced struct {
	FileMove
	Source     string
	Resolution string
	Companions []FileMove
}

type ReconcileResult struct {
	Series       refs.Series
	AppliedMoves int
}

type PlanStaleError struct {
	Series refs.Series
}

func (err PlanStaleError) Error() string {
	return fmt.Sprintf("series: reconcile plan for %s is stale", err.Series)
}

func (h Handle) PlanReconcile() (ReconcilePlan, error) {
	series, err := h.Load()
	if err != nil {
		return ReconcilePlan{}, err
	}
	snapshot, err := h.snapshot()
	if err != nil {
		return ReconcilePlan{}, err
	}
	changes, err := h.planChanges(series)
	if err != nil {
		return ReconcilePlan{}, err
	}
	if err := h.validateMoves(changes); err != nil {
		return ReconcilePlan{}, err
	}
	return ReconcilePlan{
		Series:    h.ref,
		FileTitle: h.ref.String(),
		Snapshot:  snapshot,
		Changes:   changes,
	}, nil
}

func (h Handle) ApplyReconcile(plan ReconcilePlan) (ReconcileResult, error) {
	if plan.Series != h.ref {
		return ReconcileResult{}, PlanStaleError{Series: plan.Series}
	}
	snapshot, err := h.snapshot()
	if err != nil {
		return ReconcileResult{}, err
	}
	if snapshot != plan.Snapshot {
		return ReconcileResult{}, PlanStaleError{Series: plan.Series}
	}
	if !plan.HasChanges() {
		return ReconcileResult{Series: h.ref}, nil
	}
	seriesDir, err := h.lib.files.seriesDir(h.ref)
	if err != nil {
		return ReconcileResult{}, err
	}
	var moves []FileMove
	for _, change := range plan.Changes {
		moves = append(moves, change.Moves()...)
	}
	for _, move := range moves {
		from := move.From
		if !filepath.IsAbs(from) {
			from = filepath.Join(seriesDir.Path(), filepath.FromSlash(move.From))
		}
		to := filepath.Join(seriesDir.Path(), filepath.FromSlash(move.To))
		if err := h.lib.files.move(from, to); err != nil {
			return ReconcileResult{}, err
		}
	}
	updated, err := h.applyPlanState(plan)
	if err != nil {
		return ReconcileResult{}, err
	}
	if err := h.lib.repo.save(h.ref, updated); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Series: h.ref, AppliedMoves: len(moves)}, nil
}

func (h Handle) snapshot() (string, error) {
	path := fsroot.SeriesMetadataPath(h.lib.root.Join(h.ref.String()))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:]), nil
}

func (h Handle) planChanges(series Series) ([]Change, error) {
	var refsList []refs.Episode
	for ref := range series.Episodes {
		refsList = append(refsList, ref)
	}
	sort.Slice(refsList, func(i, j int) bool { return refsList[i].String() < refsList[j].String() })
	var changes []Change
	for _, episodeRef := range refsList {
		episode := series.Episodes[episodeRef]
		if episode.Staged != nil {
			change, err := h.stagedChange(episodeRef, episode)
			if err != nil {
				return nil, err
			}
			changes = append(changes, change)
			continue
		}
		if episode.Active != nil {
			change, ok, err := h.moveChange(episodeRef, *episode.Active)
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

func (h Handle) stagedChange(episodeRef refs.Episode, episode Episode) (Change, error) {
	target, err := h.lib.files.canonicalPath(h.ref, episodeRef, *episode.Staged)
	if err != nil {
		return Change{}, err
	}
	change := Change{
		Kind:       ChangeAdd,
		Episode:    episodeRef,
		FileMove:   FileMove{From: episode.Staged.Path, To: target},
		Source:     episode.Staged.Source,
		Resolution: episode.Staged.Resolution,
		Companions: companionMoves(episodeRef, episode.Staged.Path, target, episode.Staged.Companions),
	}
	if episode.Active != nil {
		id := ulid.Make()
		change.Kind = ChangeReplace
		change.Replaced = &Replaced{
			FileMove:   FileMove{From: episode.Active.Path, To: trashRelPath(id, episode.Active.Path)},
			Source:     episode.Active.Source,
			Resolution: episode.Active.Resolution,
			Companions: trashCompanionMoves(id, episode.Active.Companions),
		}
	}
	return change, nil
}

func (h Handle) moveChange(episodeRef refs.Episode, active MediaRecord) (Change, bool, error) {
	target, err := h.lib.files.canonicalPath(h.ref, episodeRef, active)
	if err != nil {
		return Change{}, false, err
	}
	companionMoves := companionMoves(episodeRef, active.Path, target, active.Companions)
	if target == active.Path && len(companionMoves) == 0 {
		return Change{}, false, nil
	}
	return Change{
		Kind:       ChangeMove,
		Episode:    episodeRef,
		FileMove:   FileMove{From: active.Path, To: target},
		Source:     active.Source,
		Resolution: active.Resolution,
		Companions: companionMoves,
	}, true, nil
}

func (h Handle) validateMoves(changes []Change) error {
	seriesDir, err := h.lib.files.seriesDir(h.ref)
	if err != nil {
		return err
	}
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
		targetAbs := filepath.Join(seriesDir.Path(), filepath.FromSlash(target))
		sourceAbs := source
		if !filepath.IsAbs(sourceAbs) {
			sourceAbs = filepath.Join(seriesDir.Path(), filepath.FromSlash(source))
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

func (h Handle) applyPlanState(plan ReconcilePlan) (Series, error) {
	series, err := h.Load()
	if err != nil {
		return Series{}, err
	}
	edit := editor{series: &series}
	for _, change := range plan.Changes {
		episode := series.Episodes[change.Episode]
		switch change.Kind {
		case ChangeAdd, ChangeReplace:
			if episode.Staged == nil {
				return Series{}, fmt.Errorf("series: %s has no staged media", change.Episode)
			}
			if change.Replaced != nil && episode.Active != nil {
				if err := h.writeTrash(change.Episode, *episode.Active, *change.Replaced); err != nil {
					return Series{}, err
				}
			}
			episode.Staged.Path = change.To
			for index := range episode.Staged.Companions {
				if index < len(change.Companions) {
					episode.Staged.Companions[index].Path = change.Companions[index].To
				}
			}
			series.Episodes[change.Episode] = episode
			if _, err := edit.promoteStaged(change.Episode); err != nil {
				return Series{}, err
			}
		case ChangeMove:
			if episode.Active == nil {
				return Series{}, fmt.Errorf("series: %s has no active media", change.Episode)
			}
			episode.Active.Path = change.To
			for index := range episode.Active.Companions {
				if index < len(change.Companions) {
					episode.Active.Companions[index].Path = change.Companions[index].To
				}
			}
			series.Episodes[change.Episode] = episode
		default:
			return Series{}, fmt.Errorf("series: unsupported reconcile change kind %q", change.Kind)
		}
	}
	return series, nil
}

func (h Handle) writeTrash(episode refs.Episode, record MediaRecord, replaced Replaced) error {
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
	return trash.Write(h.lib.root, h.ref, trash.Meta{
		ID:        id,
		Episode:   episode,
		TrashedAt: time.Now().UTC(),
		Record:    trashRecord(record),
	})
}

func trashRecord(in MediaRecord) trash.Record {
	out := trash.Record{
		Path:       in.Path,
		Source:     in.Source,
		Resolution: in.Resolution,
		Codec:      in.Codec,
		Size:       in.Size,
		MTime:      in.MTime,
		Companions: make([]trash.Companion, 0, len(in.Companions)),
	}
	for _, companion := range in.Companions {
		out.Companions = append(out.Companions, trash.Companion{
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

func companionMoves(_ refs.Episode, oldMediaPath string, newMediaPath string, companions []CompanionRecord) []FileMove {
	oldBase := strings.TrimSuffix(filepath.Base(oldMediaPath), filepath.Ext(oldMediaPath))
	newBase := strings.TrimSuffix(filepath.Base(newMediaPath), filepath.Ext(newMediaPath))
	dir := filepath.Dir(newMediaPath)
	if dir == "." {
		dir = ""
	}
	moves := make([]FileMove, 0, len(companions))
	for _, companion := range companions {
		target := filepath.ToSlash(filepath.Join(dir, newBase+companionSuffix(filepath.Base(companion.Path), oldBase)))
		if target != companion.Path {
			moves = append(moves, FileMove{From: companion.Path, To: target})
		}
	}
	return moves
}

func trashCompanionMoves(id ulid.ULID, companions []CompanionRecord) []FileMove {
	moves := make([]FileMove, 0, len(companions))
	for _, companion := range companions {
		moves = append(moves, FileMove{From: companion.Path, To: trashRelPath(id, companion.Path)})
	}
	return moves
}

func trashRelPath(id ulid.ULID, path string) string {
	return filepath.ToSlash(filepath.Join(fsroot.KuraDir, fsroot.KuraTrashDir, id.String(), filepath.Base(path)))
}

func trashIDFromPath(path string) (ulid.ULID, error) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 3 {
		return ulid.ULID{}, fmt.Errorf("series: trash path %q missing ulid", path)
	}
	return ulid.Parse(parts[2])
}

func companionSuffix(filename string, oldMediaBase string) string {
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

package series

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series/wire"
	"github.com/wyvernzora/kura/internal/textnorm"
)

func (h Handle) PlanReconcile() (ReconcilePlan, error) {
	series, err := h.load()
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
		FileTitle: textnorm.NFC(h.ref.String()),
		Snapshot:  snapshot,
		Changes:   changes,
	}, nil
}

func (h Handle) snapshot() (string, error) {
	path := wire.SeriesMetadataPath(filepath.Join(h.root(), filepath.FromSlash(h.ref.String())))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:]), nil
}

func (h Handle) planChanges(series seriesState) ([]Change, error) {
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

func (h Handle) stagedChange(episodeRef refs.Episode, episode episodeState) (Change, error) {
	target, err := h.files().canonicalPath(h.ref, episodeRef, *episode.Staged)
	if err != nil {
		return Change{}, err
	}
	change := Change{
		Kind:       ChangeAdd,
		Episode:    episodeRef,
		FileMove:   FileMove{From: episode.Staged.Path, To: target},
		Source:     episode.Staged.Source,
		Resolution: episode.Staged.Resolution,
		Companions: companionMoves(episode.Staged.Path, target, episode.Staged.Companions),
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
	target, err := h.files().canonicalPath(h.ref, episodeRef, active)
	if err != nil {
		return Change{}, false, err
	}
	companionMoves := companionMoves(active.Path, target, active.Companions)
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
	seriesDir, err := h.files().seriesDir(h.ref)
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

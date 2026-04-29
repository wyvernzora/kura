package kura

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/store"
)

func (s *Series) PlanReconcile(_ context.Context, _ ReconcileInput) (ReconcilePlan, error) {
	seriesDir, err := s.library.root.SeriesDir(string(s.ref))
	if err != nil {
		return ReconcilePlan{}, err
	}
	snapshot, err := reconcileSnapshot(seriesDir.Path())
	if err != nil {
		return ReconcilePlan{}, err
	}
	title := domain.CleanFileTitle(seriesDir.Name())
	if _, err := domain.ParseFileTitle(title.String()); err != nil {
		return ReconcilePlan{}, err
	}
	series, err := store.LoadSeries(seriesDir.Path())
	if err != nil {
		return ReconcilePlan{}, err
	}
	staged, err := store.LoadStaged(seriesDir.Path())
	if err != nil {
		return ReconcilePlan{}, err
	}
	changes, err := planChanges(seriesDir, title, *series, *staged)
	if err != nil {
		return ReconcilePlan{}, err
	}
	if err := validatePlanMoves(seriesDir, changes); err != nil {
		return ReconcilePlan{}, err
	}
	return ReconcilePlan{
		Series:    s.ref,
		FileTitle: title.String(),
		Snapshot:  snapshot,
		Changes:   changes,
	}, nil
}

func (s *Series) ApplyReconcile(ctx context.Context, plan ReconcilePlan) (ReconcileResult, error) {
	if plan.Series != s.ref {
		return ReconcileResult{}, PlanStaleError{Series: plan.Series}
	}
	seriesDir, err := s.library.root.SeriesDir(string(s.ref))
	if err != nil {
		return ReconcileResult{}, err
	}
	snapshot, err := reconcileSnapshot(seriesDir.Path())
	if err != nil {
		return ReconcileResult{}, err
	}
	if snapshot != plan.Snapshot {
		return ReconcileResult{}, PlanStaleError{Series: plan.Series}
	}
	if !plan.HasChanges() {
		return ReconcileResult{Series: s.ref}, nil
	}

	moves := make([]FileMove, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		moves = append(moves, change.Moves()...)
	}
	progress.Start(ctx, "series-reconcile", fmt.Sprintf("Reconciling %s", plan.Series), len(moves))
	for index, move := range moves {
		if move.From == move.To {
			continue
		}
		progress.Update(ctx, "series-reconcile", fmt.Sprintf("Moving %s", move.From), index+1, len(moves))
		from := move.From
		if !filepath.IsAbs(from) {
			from = filepath.Join(seriesDir.Path(), filepath.FromSlash(move.From))
		}
		to := filepath.Join(seriesDir.Path(), filepath.FromSlash(move.To))
		if err := fsroot.SafeMoveFile(from, to); err != nil {
			progress.Failure(ctx, "series-reconcile", fmt.Sprintf("Failed moving %s", move.From), index+1, len(moves))
			return ReconcileResult{}, err
		}
	}

	updatedSeries, updatedStaged, updatedTrash, err := applyPlanState(seriesDir, plan)
	if err != nil {
		return ReconcileResult{}, err
	}
	if err := backupBefore(seriesDir.Path(), "series"); err != nil {
		return ReconcileResult{}, err
	}
	if err := store.SaveSeries(updatedSeries); err != nil {
		progress.Failure(ctx, "series-reconcile", "Failed writing series metadata", len(moves), len(moves))
		return ReconcileResult{}, err
	}
	if err := backupBefore(seriesDir.Path(), "trash"); err != nil {
		return ReconcileResult{}, err
	}
	if err := store.SaveTrash(updatedTrash); err != nil {
		progress.Failure(ctx, "series-reconcile", "Failed writing trash metadata", len(moves), len(moves))
		return ReconcileResult{}, err
	}
	if err := backupBefore(seriesDir.Path(), "staged"); err != nil {
		return ReconcileResult{}, err
	}
	if err := store.SaveStaged(updatedStaged); err != nil {
		progress.Failure(ctx, "series-reconcile", "Failed writing staged metadata", len(moves), len(moves))
		return ReconcileResult{}, err
	}
	s.record = updatedSeries
	progress.Success(ctx, "series-reconcile", fmt.Sprintf("Reconciled %d file move(s)", len(moves)), len(moves))
	return ReconcileResult{Series: s.ref, AppliedMoves: len(moves)}, nil
}

func reconcileSnapshot(seriesDir string) (string, error) {
	hash := sha256.New()
	for _, path := range []string{
		store.SeriesMetadataPath(seriesDir),
		store.StagedPath(seriesDir),
		store.TrashPath(seriesDir),
	} {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(hash, "%s:0\n", filepath.Base(path))
			continue
		}
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s:%d\n", filepath.Base(path), len(data))
		if _, err := hash.Write(data); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func planChanges(seriesDir fsroot.SeriesDir, title domain.FileTitle, series store.Series, staged store.Staged) ([]Change, error) {
	stagedBySlot := map[string]struct{}{}
	changes := make([]Change, 0, len(staged.Entries))
	entries := append([]store.StagedEpisode(nil), staged.Entries...)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Season != entries[j].Season {
			return entries[i].Season < entries[j].Season
		}
		if entries[i].Number != entries[j].Number {
			return entries[i].Number < entries[j].Number
		}
		return entries[i].Media.Path < entries[j].Media.Path
	})
	for _, stagedEpisode := range entries {
		if err := validateStagedSource(stagedEpisode); err != nil {
			return nil, err
		}
		_, episodeMoves, err := activeEpisodeFromStaged(title, stagedEpisode)
		if err != nil {
			return nil, err
		}
		if len(episodeMoves) == 0 {
			return nil, fmt.Errorf("kura: staged S%02dE%02d produced no moves", stagedEpisode.Season, stagedEpisode.Number)
		}
		change := Change{
			Kind:     ChangeAdd,
			Season:   stagedEpisode.Season,
			Episode:  stagedEpisode.Number,
			FileMove: episodeMoves[0],
			Source:   domain.ParseMediaSource(stagedEpisode.Media.Source).Display(),
		}
		if len(episodeMoves) > 1 {
			change.Companions = append([]FileMove(nil), episodeMoves[1:]...)
		}
		if stagedEpisode.Media.MediaInfo != nil {
			change.Resolution = stagedEpisode.Media.MediaInfo.Resolution
		}
		if existing, ok := series.LookupEpisode(stagedEpisode.Season, stagedEpisode.Number); ok {
			trashID := ulid.Make().String()
			change.Kind = ChangeReplace
			change.Replaced = &Replaced{
				FileMove: FileMove{
					From: existing.Media.Path,
					To:   filepath.ToSlash(filepath.Join(fsroot.KuraDir, fsroot.KuraTrashDir, trashID, filepath.Base(existing.Media.Path))),
				},
				Source: domain.ParseMediaSource(existing.Media.Source).Display(),
			}
			change.Replaced.Companions = trashCompanionMoves(trashID, existing.Companions)
			if existing.Media.MediaInfo != nil {
				change.Replaced.Resolution = existing.Media.MediaInfo.Resolution
			}
		}
		changes = append(changes, change)
		stagedBySlot[episodeKey(stagedEpisode.Season, stagedEpisode.Number)] = struct{}{}
	}

	seasons := append([]store.Season(nil), series.Seasons...)
	sort.Slice(seasons, func(i, j int) bool { return seasons[i].Number < seasons[j].Number })
	for _, season := range seasons {
		episodes := append([]store.Episode(nil), season.Episodes...)
		sort.Slice(episodes, func(i, j int) bool { return episodes[i].Number < episodes[j].Number })
		for _, episode := range episodes {
			if _, staged := stagedBySlot[episodeKey(season.Number, episode.Number)]; staged {
				continue
			}
			episodeMoves, err := reconcileEpisodeMoves(seriesDir, title, season.Number, episode.Number, episode)
			if err != nil {
				return nil, err
			}
			if len(episodeMoves) == 0 {
				continue
			}
			change := Change{
				Kind:     ChangeMove,
				Season:   season.Number,
				Episode:  episode.Number,
				FileMove: episodeMoves[0],
				Source:   domain.ParseMediaSource(episode.Media.Source).Display(),
			}
			if len(episodeMoves) > 1 {
				change.Companions = append([]FileMove(nil), episodeMoves[1:]...)
			}
			if episode.Media.MediaInfo != nil {
				change.Resolution = episode.Media.MediaInfo.Resolution
			}
			changes = append(changes, change)
		}
	}
	return changes, nil
}

func applyPlanState(seriesDir fsroot.SeriesDir, plan ReconcilePlan) (store.Series, store.Staged, store.Trash, error) {
	series, err := store.LoadSeries(seriesDir.Path())
	if err != nil {
		return store.Series{}, store.Staged{}, store.Trash{}, err
	}
	staged, err := store.LoadStaged(seriesDir.Path())
	if err != nil {
		return store.Series{}, store.Staged{}, store.Trash{}, err
	}
	trash, err := store.LoadTrash(seriesDir.Path())
	if err != nil {
		return store.Series{}, store.Staged{}, store.Trash{}, err
	}
	updatedSeries := *series
	updatedStaged := *staged
	updatedTrash := *trash

	for _, change := range plan.Changes {
		switch change.Kind {
		case ChangeAdd, ChangeReplace:
			stagedEpisode, _, ok := updatedStaged.Lookup(change.Season, change.Episode)
			if !ok {
				return store.Series{}, store.Staged{}, store.Trash{}, fmt.Errorf("kura: staged S%02dE%02d missing", change.Season, change.Episode)
			}
			if change.Replaced != nil {
				existing, ok := updatedSeries.LookupEpisode(change.Season, change.Episode)
				if !ok {
					return store.Series{}, store.Staged{}, store.Trash{}, fmt.Errorf("kura: active S%02dE%02d missing", change.Season, change.Episode)
				}
				trashed := trashedEpisodeFromPlan(change, existing)
				updatedTrash.Entries = append(updatedTrash.Entries, trashed)
			}
			episode := stagedEpisode.Episode
			episode.Media.Path = change.To
			for index := range episode.Companions {
				if index < len(change.Companions) {
					episode.Companions[index].Path = change.Companions[index].To
				}
			}
			if err := setSeriesEpisode(&updatedSeries, change.Season, change.Episode, episode); err != nil {
				return store.Series{}, store.Staged{}, store.Trash{}, err
			}
		case ChangeMove:
			episode, ok := updatedSeries.LookupEpisode(change.Season, change.Episode)
			if !ok {
				return store.Series{}, store.Staged{}, store.Trash{}, fmt.Errorf("kura: active S%02dE%02d missing", change.Season, change.Episode)
			}
			episode.Media.Path = change.To
			for index := range episode.Companions {
				if index < len(change.Companions) {
					episode.Companions[index].Path = change.Companions[index].To
				}
			}
			if err := setSeriesEpisode(&updatedSeries, change.Season, change.Episode, episode); err != nil {
				return store.Series{}, store.Staged{}, store.Trash{}, err
			}
		default:
			return store.Series{}, store.Staged{}, store.Trash{}, fmt.Errorf("kura: unsupported reconcile change kind %q", change.Kind)
		}
	}
	updatedStaged.Entries = nil
	if err := updatedSeries.Validate(); err != nil {
		return store.Series{}, store.Staged{}, store.Trash{}, err
	}
	if err := updatedStaged.Validate(); err != nil {
		return store.Series{}, store.Staged{}, store.Trash{}, err
	}
	if err := updatedTrash.Validate(); err != nil {
		return store.Series{}, store.Staged{}, store.Trash{}, err
	}
	return updatedSeries, updatedStaged, updatedTrash, nil
}

func activeEpisodeFromStaged(title domain.FileTitle, staged store.StagedEpisode) (store.Episode, []FileMove, error) {
	targetMediaPath, err := reconciledMediaPath(title, staged.Season, staged.Number, staged.Media)
	if err != nil {
		return store.Episode{}, nil, err
	}
	episode := staged.Episode
	episode.Media.Path = targetMediaPath
	moves := []FileMove{{From: staged.Media.Path, To: targetMediaPath}}

	oldMediaBase := strings.TrimSuffix(filepath.Base(staged.Media.Path), filepath.Ext(staged.Media.Path))
	newMediaBase := strings.TrimSuffix(filepath.Base(targetMediaPath), filepath.Ext(targetMediaPath))
	for index := range episode.Companions {
		companion := &episode.Companions[index]
		targetCompanionPath := filepath.ToSlash(filepath.Join(
			targetEpisodeDir(staged.Season),
			newMediaBase+companionSuffix(filepath.Base(companion.Path), oldMediaBase),
		))
		moves = append(moves, FileMove{From: companion.Path, To: targetCompanionPath})
		companion.Path = targetCompanionPath
	}
	return episode, moves, nil
}

func reconciledMediaPath(title domain.FileTitle, seasonNumber int, episodeNumber int, media store.MediaFile) (string, error) {
	filename, err := reconciledMediaFilename(title, seasonNumber, episodeNumber, media)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join(targetEpisodeDir(seasonNumber), filename)), nil
}

func reconciledMediaFilename(title domain.FileTitle, seasonNumber int, episodeNumber int, media store.MediaFile) (string, error) {
	season, err := domain.NewSeasonNumber(seasonNumber)
	if err != nil {
		return "", err
	}
	episode, err := domain.NewEpisodeNumber(episodeNumber)
	if err != nil {
		return "", err
	}
	facts := domain.MediaFilenameFacts{Source: domain.ParseMediaSource(media.Source)}
	if media.MediaInfo != nil {
		if resolution, err := domain.ParseResolution(media.MediaInfo.Resolution); err == nil {
			facts.Resolution = resolution
		}
	}
	return domain.BuildMediaFilename(title, domain.NewEpisodeRef(season, episode), facts, filepath.Ext(media.Path)).String(), nil
}

func targetEpisodeDir(seasonNumber int) string {
	if seasonNumber == 0 {
		return ""
	}
	return fmt.Sprintf("Season %d", seasonNumber)
}

func reconcileEpisodeMoves(seriesDir fsroot.SeriesDir, title domain.FileTitle, seasonNumber int, episodeNumber int, episode store.Episode) ([]FileMove, error) {
	if episode.Media.Path == "" {
		return nil, fmt.Errorf("S%02dE%02d has no media path", seasonNumber, episodeNumber)
	}
	targetMediaPath, err := reconciledMediaPath(title, seasonNumber, episodeNumber, episode.Media)
	if err != nil {
		return nil, err
	}
	moves := []FileMove{{From: episode.Media.Path, To: targetMediaPath}}
	changed := targetMediaPath != episode.Media.Path
	if targetMediaPath != episode.Media.Path {
		if _, err := os.Stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(episode.Media.Path))); err != nil {
			return nil, err
		}
	}

	oldMediaBase := strings.TrimSuffix(filepath.Base(episode.Media.Path), filepath.Ext(episode.Media.Path))
	newMediaBase := strings.TrimSuffix(filepath.Base(targetMediaPath), filepath.Ext(targetMediaPath))
	for index := range episode.Companions {
		companion := episode.Companions[index]
		targetCompanionPath := filepath.ToSlash(filepath.Join(
			targetEpisodeDir(seasonNumber),
			newMediaBase+companionSuffix(filepath.Base(companion.Path), oldMediaBase),
		))
		if targetCompanionPath == companion.Path {
			continue
		}
		changed = true
		if _, err := os.Stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(companion.Path))); err != nil {
			return nil, err
		}
		moves = append(moves, FileMove{From: companion.Path, To: targetCompanionPath})
	}
	if !changed {
		return nil, nil
	}
	return moves, nil
}

func setSeriesEpisode(series *store.Series, seasonNumber int, episodeNumber int, episode store.Episode) error {
	if seasonNumber < 0 {
		return fmt.Errorf("library: invalid season %d", seasonNumber)
	}
	if episodeNumber < 1 {
		return fmt.Errorf("library: invalid episode %d", episodeNumber)
	}
	season := store.Season{Number: seasonNumber}
	if existingSeason, ok := series.Season(seasonNumber); ok {
		season = *existingSeason
	}
	episode.Number = episodeNumber
	season.UpsertEpisode(episode)
	series.UpsertSeason(season)
	return nil
}

func validateStagedSource(staged store.StagedEpisode) error {
	if _, err := os.Stat(staged.Media.Path); err != nil {
		return err
	}
	for _, companion := range staged.Companions {
		if _, err := os.Stat(companion.Path); err != nil {
			return err
		}
	}
	return nil
}

func validatePlanMoves(seriesDir fsroot.SeriesDir, changes []Change) error {
	targets := map[string]string{}
	relativeSources := map[string]struct{}{}
	for _, change := range changes {
		for _, move := range change.Moves() {
			if move.From == move.To {
				continue
			}
			if existingSource, exists := targets[move.To]; exists && existingSource != move.From {
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

func trashedEpisodeFromPlan(change Change, episode store.Episode) store.TrashedEpisode {
	id := ""
	if change.Replaced != nil {
		parts := strings.Split(filepath.ToSlash(change.Replaced.To), "/")
		if len(parts) >= 3 {
			id = parts[2]
		}
		episode.Media.Path = change.Replaced.To
		for index := range episode.Companions {
			if index < len(change.Replaced.Companions) {
				episode.Companions[index].Path = change.Replaced.Companions[index].To
			}
		}
	}
	episode.Number = change.Episode
	return store.TrashedEpisode{
		ID:      id,
		Season:  change.Season,
		Number:  change.Episode,
		Episode: episode,
	}
}

func trashCompanionMoves(trashID string, companions []store.CompanionFile) []FileMove {
	moves := make([]FileMove, 0, len(companions))
	for _, companion := range companions {
		moves = append(moves, FileMove{
			From: companion.Path,
			To:   filepath.ToSlash(filepath.Join(fsroot.KuraDir, fsroot.KuraTrashDir, trashID, filepath.Base(companion.Path))),
		})
	}
	return moves
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

func episodeKey(seasonNumber int, episodeNumber int) string {
	return fmt.Sprintf("%d:%d", seasonNumber, episodeNumber)
}

package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/store"
)

type Plan struct {
	Series        string       `json:"series"`
	Target        string       `json:"target"`
	DryRun        bool         `json:"dryRun"`
	FileMoves     []Move       `json:"fileMoves"`
	SeriesPath    string       `json:"-"`
	UpdatedSeries store.Series `json:"-"`
	UpdatedStaged store.Staged `json:"-"`
	UpdatedTrash  store.Trash  `json:"-"`

	libraryRoot     string
	metadataChanged bool
}

func (p Plan) HasChanges() bool {
	return len(p.FileMoves) > 0 || p.metadataChanged
}

type Move struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func PlanSeries(_ context.Context, root fsroot.LibraryRoot, dirname string) (Plan, error) {
	seriesDir, err := root.SeriesDir(dirname)
	if err != nil {
		return Plan{}, err
	}
	loaded, err := store.LoadSeries(seriesDir.Path())
	if err != nil {
		return Plan{}, err
	}
	title := domain.CleanFileTitle(seriesDir.Name())
	if _, err := domain.ParseFileTitle(title.String()); err != nil {
		return Plan{}, err
	}

	updated := *loaded
	staged, err := store.LoadStaged(seriesDir.Path())
	if err != nil {
		return Plan{}, err
	}
	updatedStaged := *staged
	trash, err := store.LoadTrash(seriesDir.Path())
	if err != nil {
		return Plan{}, err
	}
	updatedTrash := *trash
	stagedMoves, stagedChanged, err := applyStagedEpisodes(title, &updated, &updatedStaged, &updatedTrash)
	if err != nil {
		return Plan{}, err
	}
	fileMoves, err := reconcileEpisodes(seriesDir, title, &updated, &updatedTrash)
	if err != nil {
		return Plan{}, err
	}
	fileMoves = append(fileMoves, stagedMoves...)
	if err := validateMoves(seriesDir, fileMoves); err != nil {
		return Plan{}, err
	}

	return Plan{
		Series:          seriesDir.Name(),
		Target:          title.String(),
		FileMoves:       fileMoves,
		SeriesPath:      seriesDir.Name(),
		UpdatedSeries:   updated,
		UpdatedStaged:   updatedStaged,
		UpdatedTrash:    updatedTrash,
		libraryRoot:     root.Path(),
		metadataChanged: stagedChanged,
	}, nil
}

func ApplyPlan(ctx context.Context, plan Plan) error {
	if !plan.HasChanges() {
		return nil
	}
	progress.Start(ctx, "series-reconcile", fmt.Sprintf("Reconciling %s", plan.Series), len(plan.FileMoves))
	seriesDir := filepath.Join(plan.libraryRoot, filepath.FromSlash(plan.SeriesPath))
	for index, move := range plan.FileMoves {
		if move.From == move.To {
			continue
		}
		progress.Update(ctx, "series-reconcile", fmt.Sprintf("Moving %s", move.From), index+1, len(plan.FileMoves))
		from := move.From
		if !filepath.IsAbs(from) {
			from = filepath.Join(seriesDir, filepath.FromSlash(move.From))
		}
		to := filepath.Join(seriesDir, filepath.FromSlash(move.To))
		if err := fsroot.SafeMoveFile(from, to); err != nil {
			progress.Failure(ctx, "series-reconcile", fmt.Sprintf("Failed moving %s", move.From), index+1, len(plan.FileMoves))
			return err
		}
	}
	progress.Update(ctx, "series-reconcile", fmt.Sprintf("Writing series metadata: %s", store.SeriesMetadataPath(seriesDir)), len(plan.FileMoves), len(plan.FileMoves))
	if err := store.SaveSeries(plan.UpdatedSeries); err != nil {
		progress.Failure(ctx, "series-reconcile", "Failed writing series metadata", len(plan.FileMoves), len(plan.FileMoves))
		return err
	}
	if err := store.SaveTrash(plan.UpdatedTrash); err != nil {
		progress.Failure(ctx, "series-reconcile", "Failed writing trash metadata", len(plan.FileMoves), len(plan.FileMoves))
		return err
	}
	if err := store.SaveStaged(plan.UpdatedStaged); err != nil {
		progress.Failure(ctx, "series-reconcile", "Failed writing staged metadata", len(plan.FileMoves), len(plan.FileMoves))
		return err
	}
	progress.Success(ctx, "series-reconcile", fmt.Sprintf("Reconciled %d file move(s)", len(plan.FileMoves)), len(plan.FileMoves))
	return nil
}

func applyStagedEpisodes(title domain.FileTitle, series *store.Series, staged *store.Staged, trash *store.Trash) ([]Move, bool, error) {
	if staged.IsEmpty() {
		return nil, false, nil
	}
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

	moves := make([]Move, 0, len(entries))
	for _, stagedEpisode := range entries {
		if err := validateStagedSource(stagedEpisode); err != nil {
			return nil, false, err
		}
		existing, exists := series.LookupEpisode(stagedEpisode.Season, stagedEpisode.Number)
		if exists {
			trash.Entries = append(trash.Entries, store.NewTrashedEpisode(stagedEpisode.Season, stagedEpisode.Number, existing))
		}
		episode, episodeMoves, err := activeEpisodeFromStaged(title, stagedEpisode)
		if err != nil {
			return nil, false, err
		}
		if err := setSeriesEpisode(series, stagedEpisode.Season, stagedEpisode.Number, episode); err != nil {
			return nil, false, err
		}
		moves = append(moves, episodeMoves...)
	}
	staged.Entries = nil
	if err := series.Validate(); err != nil {
		return nil, false, err
	}
	if err := staged.Validate(); err != nil {
		return nil, false, err
	}
	if err := trash.Validate(); err != nil {
		return nil, false, err
	}
	return moves, true, nil
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

func activeEpisodeFromStaged(title domain.FileTitle, staged store.StagedEpisode) (store.Episode, []Move, error) {
	targetMediaFilename, err := reconciledMediaFilename(title, staged.Season, staged.Number, staged.Media)
	if err != nil {
		return store.Episode{}, nil, err
	}
	targetMediaPath := filepath.ToSlash(filepath.Join(targetEpisodeDir(staged.Season), targetMediaFilename))
	episode := staged.Episode
	episode.Media.Path = targetMediaPath
	moves := []Move{{From: staged.Media.Path, To: targetMediaPath}}

	oldMediaBase := strings.TrimSuffix(filepath.Base(staged.Media.Path), filepath.Ext(staged.Media.Path))
	newMediaBase := strings.TrimSuffix(filepath.Base(targetMediaPath), filepath.Ext(targetMediaPath))
	for index := range episode.Companions {
		companion := &episode.Companions[index]
		targetCompanionPath := filepath.ToSlash(filepath.Join(
			targetEpisodeDir(staged.Season),
			newMediaBase+companionSuffix(filepath.Base(companion.Path), oldMediaBase),
		))
		moves = append(moves, Move{From: companion.Path, To: targetCompanionPath})
		companion.Path = targetCompanionPath
	}
	return episode, moves, nil
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

func reconcileEpisodes(seriesDir fsroot.SeriesDir, title domain.FileTitle, series *store.Series, trash *store.Trash) ([]Move, error) {
	var moves []Move
	seasons := append([]store.Season(nil), series.Seasons...)
	sort.Slice(seasons, func(i, j int) bool {
		return seasons[i].Number < seasons[j].Number
	})
	for _, season := range seasons {
		if season.Number < 0 {
			continue
		}
		seasonMoves, err := reconcileSeasonEpisodes(seriesDir, title, season.Number, &season)
		if err != nil {
			return nil, err
		}
		moves = append(moves, seasonMoves...)
		series.UpsertSeason(season)
	}
	trashMoves, err := reconcileTrash(seriesDir, trash)
	if err != nil {
		return nil, err
	}
	moves = append(moves, trashMoves...)
	return moves, nil
}

func reconcileSeasonEpisodes(seriesDir fsroot.SeriesDir, title domain.FileTitle, seasonNumber int, season *store.Season) ([]Move, error) {
	var moves []Move
	episodes := append([]store.Episode(nil), season.Episodes...)
	sort.Slice(episodes, func(i, j int) bool {
		return episodes[i].Number < episodes[j].Number
	})
	for _, episode := range episodes {
		if episode.Number < 1 {
			continue
		}
		episodeMoves, err := reconcileEpisode(seriesDir, title, seasonNumber, episode.Number, &episode)
		if err != nil {
			return nil, err
		}
		moves = append(moves, episodeMoves...)
		season.UpsertEpisode(episode)
	}
	return moves, nil
}

func reconcileEpisode(seriesDir fsroot.SeriesDir, title domain.FileTitle, seasonNumber int, episodeNumber int, episode *store.Episode) ([]Move, error) {
	var moves []Move
	mediaFile := episode.Media
	if mediaFile.Path == "" {
		return nil, fmt.Errorf("S%02dE%02d has no media path", seasonNumber, episodeNumber)
	}
	targetMediaFilename, err := reconciledMediaFilename(title, seasonNumber, episodeNumber, mediaFile)
	if err != nil {
		return nil, err
	}
	targetMediaPath := filepath.ToSlash(filepath.Join(
		targetEpisodeDir(seasonNumber),
		targetMediaFilename,
	))
	if targetMediaPath != mediaFile.Path {
		if _, err := os.Stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(mediaFile.Path))); err != nil {
			return nil, err
		}
		moves = append(moves, Move{From: mediaFile.Path, To: targetMediaPath})
		episode.Media.Path = targetMediaPath
	}

	oldMediaBase := strings.TrimSuffix(filepath.Base(mediaFile.Path), filepath.Ext(mediaFile.Path))
	newMediaBase := strings.TrimSuffix(filepath.Base(targetMediaPath), filepath.Ext(targetMediaPath))
	for index := range episode.Companions {
		companion := &episode.Companions[index]
		targetCompanionPath := filepath.ToSlash(filepath.Join(
			targetEpisodeDir(seasonNumber),
			newMediaBase+companionSuffix(filepath.Base(companion.Path), oldMediaBase),
		))
		if targetCompanionPath == companion.Path {
			continue
		}
		if _, err := os.Stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(companion.Path))); err != nil {
			return nil, err
		}
		moves = append(moves, Move{From: companion.Path, To: targetCompanionPath})
		companion.Path = targetCompanionPath
	}
	return moves, nil
}

func reconcileTrash(seriesDir fsroot.SeriesDir, trash *store.Trash) ([]Move, error) {
	var moves []Move
	for index := range trash.Entries {
		trashed := &trash.Entries[index]
		if trashed.ID == "" {
			return nil, errors.New("trashed episode has no id")
		}
		targetMediaPath := filepath.ToSlash(filepath.Join(fsroot.KuraDir, fsroot.KuraTrashDir, trashed.ID, filepath.Base(trashed.Media.Path)))
		if targetMediaPath != trashed.Media.Path {
			if _, err := os.Stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(trashed.Media.Path))); err != nil {
				return nil, err
			}
			moves = append(moves, Move{From: trashed.Media.Path, To: targetMediaPath})
			trashed.Media.Path = targetMediaPath
		}
		for companionIndex := range trashed.Companions {
			companion := &trashed.Companions[companionIndex]
			targetCompanionPath := filepath.ToSlash(filepath.Join(fsroot.KuraDir, fsroot.KuraTrashDir, trashed.ID, filepath.Base(companion.Path)))
			if targetCompanionPath == companion.Path {
				continue
			}
			if _, err := os.Stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(companion.Path))); err != nil {
				return nil, err
			}
			moves = append(moves, Move{From: companion.Path, To: targetCompanionPath})
			companion.Path = targetCompanionPath
		}
	}
	return moves, nil
}

func targetEpisodeDir(seasonNumber int) string {
	if seasonNumber == 0 {
		return ""
	}
	return fmt.Sprintf("Season %d", seasonNumber)
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
	facts := domain.MediaFilenameFacts{
		Source: domain.ParseMediaSource(media.Source),
	}
	if media.MediaInfo != nil {
		if resolution, err := domain.ParseResolution(media.MediaInfo.Resolution); err == nil {
			facts.Resolution = resolution
		}
	}
	return domain.BuildMediaFilename(title, domain.NewEpisodeRef(season, episode), facts, filepath.Ext(media.Path)).String(), nil
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

func validateMoves(seriesDir fsroot.SeriesDir, moves []Move) error {
	targets := map[string]string{}
	relativeSources := map[string]struct{}{}
	for _, move := range moves {
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

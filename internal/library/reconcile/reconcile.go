package reconcile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	mediafacts "github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/library/layout"
	"github.com/wyvernzora/kura/internal/library/models"
	"github.com/wyvernzora/kura/internal/progress"
)

type Plan struct {
	Series        string        `json:"series"`
	Target        string        `json:"target"`
	DryRun        bool          `json:"dryRun"`
	FileMoves     []Move        `json:"fileMoves"`
	SeriesDir     string        `json:"-"`
	UpdatedSeries models.Series `json:"-"`
	UpdatedStaged models.Staged `json:"-"`
	UpdatedTrash  models.Trash  `json:"-"`

	metadataChanged bool
}

func (p Plan) HasChanges() bool {
	return len(p.FileMoves) > 0 || p.metadataChanged
}

type Move struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func PlanSeries(_ context.Context, root layout.LibraryRoot, dirname string, store models.Store) (Plan, error) {
	seriesDir, err := root.SeriesDir(dirname)
	if err != nil {
		return Plan{}, err
	}
	loaded, err := store.Load(seriesDir.Path())
	if err != nil {
		return Plan{}, err
	}
	title := mediafacts.CleanFilesystemTitle(seriesDir.Name())
	if _, err := mediafacts.ParseFilesystemTitle(title.String()); err != nil {
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
		SeriesDir:       seriesDir.Path(),
		UpdatedSeries:   updated,
		UpdatedStaged:   updatedStaged,
		UpdatedTrash:    updatedTrash,
		metadataChanged: stagedChanged,
	}, nil
}

func ApplyPlan(ctx context.Context, plan Plan, store models.Store) error {
	if !plan.HasChanges() {
		return nil
	}
	progress.Start(ctx, "series-reconcile", fmt.Sprintf("Reconciling %s", plan.Series), len(plan.FileMoves))
	for index, move := range plan.FileMoves {
		if move.From == move.To {
			continue
		}
		progress.Update(ctx, "series-reconcile", fmt.Sprintf("Moving %s", move.From), index+1, len(plan.FileMoves))
		from := move.From
		if !filepath.IsAbs(from) {
			from = filepath.Join(plan.SeriesDir, filepath.FromSlash(move.From))
		}
		to := filepath.Join(plan.SeriesDir, filepath.FromSlash(move.To))
		if err := safeMoveFile(from, to); err != nil {
			progress.Failure(ctx, "series-reconcile", fmt.Sprintf("Failed moving %s", move.From), index+1, len(plan.FileMoves))
			return err
		}
	}
	progress.Update(ctx, "series-reconcile", fmt.Sprintf("Writing series metadata: %s", models.SeriesPath(plan.SeriesDir)), len(plan.FileMoves), len(plan.FileMoves))
	if err := store.Save(plan.UpdatedSeries); err != nil {
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

func applyStagedEpisodes(title mediafacts.FilesystemTitle, series *models.Series, staged *models.Staged, trash *models.Trash) ([]Move, bool, error) {
	if staged.IsEmpty() {
		return nil, false, nil
	}
	entries := append([]models.StagedEpisode(nil), staged.Entries...)
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
			trash.Entries = append(trash.Entries, models.NewTrashedEpisode(stagedEpisode.Season, stagedEpisode.Number, existing))
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

func validateStagedSource(staged models.StagedEpisode) error {
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

func activeEpisodeFromStaged(title mediafacts.FilesystemTitle, staged models.StagedEpisode) (models.Episode, []Move, error) {
	targetMediaFilename, err := reconciledMediaFilename(title, staged.Season, staged.Number, staged.Media)
	if err != nil {
		return models.Episode{}, nil, err
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

func setSeriesEpisode(series *models.Series, seasonNumber int, episodeNumber int, episode models.Episode) error {
	if seasonNumber < 0 {
		return fmt.Errorf("library: invalid season %d", seasonNumber)
	}
	if episodeNumber < 1 {
		return fmt.Errorf("library: invalid episode %d", episodeNumber)
	}
	episodeKey := strconv.Itoa(episodeNumber)
	if seasonNumber == 0 {
		season := models.Season{}
		if series.Specials != nil {
			season = *series.Specials
		}
		if season.Episodes == nil {
			season.Episodes = map[string]models.Episode{}
		}
		season.Episodes[episodeKey] = episode
		series.Specials = &season
		return nil
	}
	if series.Seasons == nil {
		series.Seasons = map[string]models.Season{}
	}
	seasonKey := strconv.Itoa(seasonNumber)
	season := series.Seasons[seasonKey]
	if season.Episodes == nil {
		season.Episodes = map[string]models.Episode{}
	}
	season.Episodes[episodeKey] = episode
	series.Seasons[seasonKey] = season
	return nil
}

func reconcileEpisodes(seriesDir layout.SeriesDir, title mediafacts.FilesystemTitle, series *models.Series, trash *models.Trash) ([]Move, error) {
	var moves []Move
	regularKeys := make([]int, 0, len(series.Seasons))
	for key := range series.Seasons {
		seasonNumber, err := strconv.Atoi(key)
		if err != nil || seasonNumber < 1 {
			continue
		}
		regularKeys = append(regularKeys, seasonNumber)
	}
	sort.Ints(regularKeys)
	for _, seasonNumber := range regularKeys {
		key := strconv.Itoa(seasonNumber)
		season := series.Seasons[key]
		seasonMoves, err := reconcileSeasonEpisodes(seriesDir, title, seasonNumber, &season)
		if err != nil {
			return nil, err
		}
		moves = append(moves, seasonMoves...)
		series.Seasons[key] = season
	}
	if series.Specials != nil {
		seasonMoves, err := reconcileSeasonEpisodes(seriesDir, title, 0, series.Specials)
		if err != nil {
			return nil, err
		}
		moves = append(moves, seasonMoves...)
	}
	trashMoves, err := reconcileTrash(seriesDir, trash)
	if err != nil {
		return nil, err
	}
	moves = append(moves, trashMoves...)
	return moves, nil
}

func reconcileSeasonEpisodes(seriesDir layout.SeriesDir, title mediafacts.FilesystemTitle, seasonNumber int, season *models.Season) ([]Move, error) {
	var moves []Move
	episodeNumbers := make([]int, 0, len(season.Episodes))
	for key := range season.Episodes {
		episodeNumber, err := strconv.Atoi(key)
		if err != nil || episodeNumber < 1 {
			continue
		}
		episodeNumbers = append(episodeNumbers, episodeNumber)
	}
	sort.Ints(episodeNumbers)
	for _, episodeNumber := range episodeNumbers {
		key := strconv.Itoa(episodeNumber)
		episode := season.Episodes[key]
		episodeMoves, err := reconcileEpisode(seriesDir, title, seasonNumber, episodeNumber, &episode)
		if err != nil {
			return nil, err
		}
		moves = append(moves, episodeMoves...)
		season.Episodes[key] = episode
	}
	return moves, nil
}

func reconcileEpisode(seriesDir layout.SeriesDir, title mediafacts.FilesystemTitle, seasonNumber int, episodeNumber int, episode *models.Episode) ([]Move, error) {
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

func reconcileTrash(seriesDir layout.SeriesDir, trash *models.Trash) ([]Move, error) {
	var moves []Move
	for index := range trash.Entries {
		trashed := &trash.Entries[index]
		if trashed.ID == "" {
			return nil, errors.New("trashed episode has no id")
		}
		targetMediaPath := filepath.ToSlash(filepath.Join(layout.KuraDir, layout.KuraTrashDir, trashed.ID, filepath.Base(trashed.Media.Path)))
		if targetMediaPath != trashed.Media.Path {
			if _, err := os.Stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(trashed.Media.Path))); err != nil {
				return nil, err
			}
			moves = append(moves, Move{From: trashed.Media.Path, To: targetMediaPath})
			trashed.Media.Path = targetMediaPath
		}
		for companionIndex := range trashed.Companions {
			companion := &trashed.Companions[companionIndex]
			targetCompanionPath := filepath.ToSlash(filepath.Join(layout.KuraDir, layout.KuraTrashDir, trashed.ID, filepath.Base(companion.Path)))
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

func reconciledMediaFilename(title mediafacts.FilesystemTitle, seasonNumber int, episodeNumber int, media models.MediaFile) (string, error) {
	season, err := mediafacts.NewSeasonNumber(seasonNumber)
	if err != nil {
		return "", err
	}
	episode, err := mediafacts.NewEpisodeNumber(episodeNumber)
	if err != nil {
		return "", err
	}
	facts := mediafacts.MediaFilenameFacts{
		Source: mediafacts.ParseMediaSource(media.Source),
	}
	if media.MediaInfo != nil {
		if resolution, err := mediafacts.ParseResolution(media.MediaInfo.Resolution); err == nil {
			facts.Resolution = resolution
		}
	}
	return mediafacts.BuildMediaFilename(title, mediafacts.NewEpisodeRef(season, episode), facts, filepath.Ext(media.Path)).String(), nil
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

func validateMoves(seriesDir layout.SeriesDir, moves []Move) error {
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

func safeMoveFile(from string, to string) error {
	if from == to {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.Rename(from, to); err == nil {
		return syncDir(filepath.Dir(to))
	} else if !isCrossDeviceMove(err) {
		return err
	}
	return copyThenRemove(from, to)
}

func copyThenRemove(from string, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("library: cannot move directory %q as file", from)
	}

	targetDir := filepath.Dir(to)
	tmp, err := os.CreateTemp(targetDir, "."+filepath.Base(to)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmp, source); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(info.Mode()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chtimes(tmpName, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	if err := os.Rename(tmpName, to); err != nil {
		return err
	}
	removeTmp = false
	if err := syncDir(targetDir); err != nil {
		return err
	}
	if err := os.Remove(from); err != nil {
		return err
	}
	return syncDir(filepath.Dir(from))
}

func isCrossDeviceMove(err error) bool {
	linkErr, ok := errors.AsType[*os.LinkError](err)
	if !ok {
		return false
	}
	return errors.Is(linkErr.Err, syscall.EXDEV)
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil
		}
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

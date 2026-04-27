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

	"github.com/wyvernzora/kura/internal/library/layout"
	mediafacts "github.com/wyvernzora/kura/internal/library/media"
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
	UpdatedTrash  models.Trash  `json:"-"`
}

func (p Plan) HasChanges() bool {
	return len(p.FileMoves) > 0
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
	title := layout.CleanFilesystemTitle(seriesDir.Name())
	if _, err := layout.ParseFilesystemTitle(title.String()); err != nil {
		return Plan{}, err
	}

	updated := *loaded
	trash, err := store.LoadTrash(seriesDir.Path())
	if err != nil {
		return Plan{}, err
	}
	updatedTrash := *trash
	fileMoves, err := reconcileEpisodes(seriesDir, title, &updated, &updatedTrash)
	if err != nil {
		return Plan{}, err
	}
	if err := validateMoves(seriesDir, fileMoves); err != nil {
		return Plan{}, err
	}

	return Plan{
		Series:        seriesDir.Name(),
		Target:        title.String(),
		FileMoves:     fileMoves,
		SeriesDir:     seriesDir.Path(),
		UpdatedSeries: updated,
		UpdatedTrash:  updatedTrash,
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
		from := filepath.Join(plan.SeriesDir, filepath.FromSlash(move.From))
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
	progress.Success(ctx, "series-reconcile", fmt.Sprintf("Reconciled %d file move(s)", len(plan.FileMoves)), len(plan.FileMoves))
	return nil
}

func reconcileEpisodes(seriesDir layout.SeriesDir, title layout.FilesystemTitle, series *models.Series, trash *models.Trash) ([]Move, error) {
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

func reconcileSeasonEpisodes(seriesDir layout.SeriesDir, title layout.FilesystemTitle, seasonNumber int, season *models.Season) ([]Move, error) {
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

func reconcileEpisode(seriesDir layout.SeriesDir, title layout.FilesystemTitle, seasonNumber int, episodeNumber int, episode *models.Episode) ([]Move, error) {
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

func reconciledMediaFilename(title layout.FilesystemTitle, seasonNumber int, episodeNumber int, media models.MediaFile) (string, error) {
	season, err := layout.NewSeasonNumber(seasonNumber)
	if err != nil {
		return "", err
	}
	episode, err := layout.NewEpisodeNumber(episodeNumber)
	if err != nil {
		return "", err
	}
	facts := layout.MediaFilenameFacts{
		Source: mediafacts.ParseMediaSource(media.Source),
	}
	if media.MediaInfo != nil {
		if resolution, err := mediafacts.ParseResolution(media.MediaInfo.Resolution); err == nil {
			facts.Resolution = resolution
		}
	}
	return layout.BuildMediaFilename(title, layout.NewEpisodeRef(season, episode), facts, filepath.Ext(media.Path)).String(), nil
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
	for _, move := range moves {
		if move.From == move.To {
			continue
		}
		if existingSource, exists := targets[move.To]; exists && existingSource != move.From {
			return fmt.Errorf("multiple tracked files target %q", move.To)
		}
		targets[move.To] = move.From
	}
	for target, source := range targets {
		if _, err := os.Stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(target))); err == nil && target != source {
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

package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// StageInput parameters for the Stage workflow. MediaPath and any
// CompanionPaths must be absolute. Replace=true allows overwriting an
// existing active or staged record on the same episode slot.
type StageInput struct {
	Ref            refs.Series
	Episode        refs.Episode
	MediaPath      string
	Source         string
	CompanionPaths []string
	Replace        bool
}

// Stage records a staged media file for one episode in the series's
// series.json. The file itself is not moved; only the staged record is
// written. Reconcile later promotes staged into active.
func Stage(ctx context.Context, deps Deps, in StageInput) (response.StageResult, error) {
	progress.Start(ctx, "stage", fmt.Sprintf("Staging %s", in.Episode), 0)
	failProgress := func() {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 1, 0)
	}

	mediaPath, err := cleanAbsoluteFilePath(in.MediaPath)
	if err != nil {
		failProgress()
		return response.StageResult{}, err
	}
	if !mediainfo.RecognizedVideoFile(mediaPath) {
		failProgress()
		return response.StageResult{}, fmt.Errorf("episode path %q is not a recognized video file", mediaPath)
	}

	seriesRoot := paths.SeriesDir(deps.LibRoot, in.Ref)
	var out response.StageResult
	err = deps.Coordinator.WithSeriesRetry(in.Ref, func() error {
		model, err := seriesfile.Load(deps.LibRoot, in.Ref)
		if err != nil {
			return err
		}
		if model.InProgress != nil {
			return &coord.BusyError{Scope: coord.SeriesScope(in.Ref), Holder: *model.InProgress}
		}
		episode, ok := model.Episodes[in.Episode]
		if !ok {
			return &MetadataMissingEpisodeError{Episode: in.Episode}
		}
		// Active collision: an active record at a different path requires --replace.
		// Same-path re-stage is a metadata refresh (e.g. updating Source) and is
		// allowed without --replace; reconcile detects this and skips the trash
		// step. Same applies when the active file is missing from disk.
		if episode.Active != nil && episode.Active.Path != mediaPath && !in.Replace {
			if _, statErr := os.Stat(episode.Active.Path); statErr == nil {
				return &EpisodeAlreadyExistsError{Episode: in.Episode}
			}
		}
		if episode.Staged != nil && !in.Replace {
			return &StagedEpisodeAlreadyExistsError{Episode: in.Episode}
		}

		progress.Update(ctx, "stage", fmt.Sprintf("Inspecting %s", filepath.Base(mediaPath)), 1, 0)
		builderInput := mediainfo.Input{
			MediaPath:  mediaPath,
			RecordPath: mediaPath,
			Source:     in.Source,
		}
		for _, companion := range in.CompanionPaths {
			path, err := cleanAbsoluteFilePath(companion)
			if err != nil {
				return err
			}
			builderInput.CompanionPaths = append(builderInput.CompanionPaths, mediainfo.CompanionInput{
				MediaPath:  path,
				RecordPath: path,
			})
		}
		record, err := mediainfo.NewBuilder(deps.Inspector).Build(ctx, builderInput)
		if err != nil {
			return err
		}
		replaced := episode.Active != nil || episode.Staged != nil
		if err := model.SetStaged(in.Episode, record); err != nil {
			return err
		}
		if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("stage")); err != nil {
			return err
		}
		out = response.StageResult{
			Replaced: replaced,
			Record:   mediaShow(seriesRoot, record),
		}
		return nil
	})
	if err != nil {
		failProgress()
		return response.StageResult{}, err
	}
	progress.Success(ctx, "stage", fmt.Sprintf("Staged %s", in.Episode), 1)
	return out, nil
}

func cleanAbsoluteFilePath(path string) (string, error) {
	path = filepath.Clean(path)
	if path == "." {
		return "", errors.New("path is required")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path %q must be absolute", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("path %q is a directory", path)
	}
	return path, nil
}

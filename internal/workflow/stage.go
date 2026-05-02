package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/series/layout"
	"github.com/wyvernzora/kura/internal/series/mediarecord"
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
	model, err := seriesfile.Load(deps.LibRoot, in.Ref)
	if err != nil {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 0, 0)
		return response.StageResult{}, err
	}
	episode, ok := model.Episodes[in.Episode]
	if !ok {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 0, 0)
		return response.StageResult{}, &MetadataMissingEpisodeError{Episode: in.Episode}
	}
	mediaPath, err := cleanAbsoluteFilePath(in.MediaPath)
	if err != nil {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 0, 0)
		return response.StageResult{}, err
	}
	if !mediarecord.RecognizedVideoFile(mediaPath) {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 0, 0)
		return response.StageResult{}, fmt.Errorf("episode path %q is not a recognized video file", mediaPath)
	}
	// Active collision: an active record at a different path requires --replace.
	// Same-path re-stage is a metadata refresh (e.g. updating Source) and is
	// allowed without --replace; reconcile detects this and skips the trash
	// step. Same applies when the active file is missing from disk.
	if episode.Active != nil && episode.Active.Path != mediaPath && !in.Replace {
		if _, statErr := os.Stat(episode.Active.Path); statErr == nil {
			progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 0, 0)
			return response.StageResult{}, &EpisodeAlreadyExistsError{Episode: in.Episode}
		}
	}
	if episode.Staged != nil && !in.Replace {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 0, 0)
		return response.StageResult{}, &StagedEpisodeAlreadyExistsError{Episode: in.Episode}
	}
	progress.Update(ctx, "stage", fmt.Sprintf("Inspecting %s", filepath.Base(mediaPath)), 1, 0)
	builderInput := mediarecord.Input{
		MediaPath:  mediaPath,
		RecordPath: mediaPath,
		Source:     in.Source,
	}
	for _, companion := range in.CompanionPaths {
		path, err := cleanAbsoluteFilePath(companion)
		if err != nil {
			progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 1, 0)
			return response.StageResult{}, err
		}
		builderInput.CompanionPaths = append(builderInput.CompanionPaths, mediarecord.CompanionInput{
			MediaPath:  path,
			RecordPath: path,
		})
	}
	record, err := mediarecord.NewBuilder(layout.NewFiles(deps.LibRoot), deps.Inspector).Build(ctx, builderInput)
	if err != nil {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 1, 0)
		return response.StageResult{}, err
	}
	replaced := episode.Active != nil || episode.Staged != nil
	if err := model.SetStaged(in.Episode, record); err != nil {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 1, 0)
		return response.StageResult{}, err
	}
	if err := seriesfile.Save(deps.LibRoot, model); err != nil {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 1, 0)
		return response.StageResult{}, err
	}
	progress.Success(ctx, "stage", fmt.Sprintf("Staged %s", in.Episode), 1)
	return response.StageResult{
		Series:   in.Ref,
		Applied:  true,
		Replaced: replaced,
		Episode:  in.Episode,
		Record:   mediaShow(record),
	}, nil
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

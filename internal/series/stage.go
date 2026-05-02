package series

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/series/layout"
	"github.com/wyvernzora/kura/internal/series/mediarecord"
)

type StageInput struct {
	Episode    refs.Episode
	MediaPath  string
	Source     string
	Companions []string
	Replace    bool
}

type StageResult struct {
	Series   refs.Series  `json:"series"`
	Applied  bool         `json:"applied"`
	Replaced bool         `json:"replaced"`
	Episode  refs.Episode `json:"episode"`
	Record   MediaRecord  `json:"record"`
}

type StagedEpisodeAlreadyExistsError struct {
	Episode refs.Episode
}

func (err StagedEpisodeAlreadyExistsError) Error() string {
	return fmt.Sprintf("staged episode %s already exists; pass replace to replace it", err.Episode.Marker())
}

func (h Handle) Stage(ctx context.Context, in StageInput) (StageResult, error) {
	progress.Start(ctx, "stage", fmt.Sprintf("Staging %s", in.Episode), 0)
	series, err := h.load()
	if err != nil {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 0, 0)
		return StageResult{}, err
	}
	episode, ok := series.Episodes[in.Episode]
	if !ok {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 0, 0)
		return StageResult{}, MetadataMissingEpisodeError{Episode: in.Episode}
	}
	if episode.Active != nil && !in.Replace {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 0, 0)
		return StageResult{}, EpisodeAlreadyExistsError{Episode: in.Episode}
	}
	if episode.Staged != nil && !in.Replace {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 0, 0)
		return StageResult{}, StagedEpisodeAlreadyExistsError{Episode: in.Episode}
	}
	mediaPath, err := cleanAbsoluteFilePath(in.MediaPath)
	if err != nil {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 0, 0)
		return StageResult{}, err
	}
	if !mediarecord.RecognizedVideoFile(mediaPath) {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 0, 0)
		return StageResult{}, fmt.Errorf("episode path %q is not a recognized video file", mediaPath)
	}
	progress.Update(ctx, "stage", fmt.Sprintf("Inspecting %s", filepath.Base(mediaPath)), 1, 0)
	record, err := h.stagedRecord(ctx, mediaPath, in.Source, in.Companions)
	if err != nil {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 1, 0)
		return StageResult{}, err
	}
	replaced := episode.Active != nil || episode.Staged != nil
	editor := editor{series: &series}
	if err := editor.setStaged(in.Episode, record); err != nil {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 1, 0)
		return StageResult{}, err
	}
	if err := h.repo().save(h.ref, series); err != nil {
		progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage %s", in.Episode), 1, 0)
		return StageResult{}, err
	}
	progress.Success(ctx, "stage", fmt.Sprintf("Staged %s", in.Episode), 1)
	return StageResult{
		Series:   h.ref,
		Applied:  true,
		Replaced: replaced,
		Episode:  in.Episode,
		Record:   record,
	}, nil
}

func (h Handle) stagedRecord(ctx context.Context, mediaPath string, source string, companions []string) (MediaRecord, error) {
	input := mediarecord.Input{
		MediaPath:  mediaPath,
		RecordPath: mediaPath,
		Source:     source,
	}
	for _, companion := range companions {
		path, err := cleanAbsoluteFilePath(companion)
		if err != nil {
			return MediaRecord{}, err
		}
		input.CompanionPaths = append(input.CompanionPaths, mediarecord.CompanionInput{
			MediaPath:  path,
			RecordPath: path,
		})
	}
	return mediarecord.NewBuilder(layout.NewFiles(h.root()), h.inspector()).Build(ctx, input)
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

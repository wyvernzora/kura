package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/store"
)

type StageEpisodeFileOptions struct {
	Season           domain.SeasonNumber
	Episode          domain.EpisodeNumber
	Source           domain.MediaSource
	Companions       []string
	MediaPath        string
	Inspector        MediaInspector
	ProviderSeries   *metadata.Series
	ProviderResolver ProviderSeriesResolver
	Apply            bool
	Replace          bool
}

type StageEpisodeFileResult struct {
	Series        string              `json:"series"`
	DryRun        bool                `json:"dryRun"`
	Replaced      bool                `json:"replaced"`
	Entry         store.StagedEpisode `json:"entry"`
	UpdatedStaged store.Staged        `json:"-"`
}

type StagedEpisodeAlreadyExistsError struct {
	Season  int
	Episode int
}

func (err StagedEpisodeAlreadyExistsError) Error() string {
	return fmt.Sprintf("staged episode S%02dE%02d already exists; pass --replace to replace it", err.Season, err.Episode)
}

func StageEpisodeFile(ctx context.Context, repo store.Repo, root fsroot.LibraryRoot, dirname string, opts StageEpisodeFileOptions) (StageEpisodeFileResult, error) {
	seriesDir, err := resolveSeriesForWorkflow(root, dirname)
	if err != nil {
		return StageEpisodeFileResult{}, err
	}
	series, err := repo.LoadSeries(seriesDir.Path())
	if err != nil {
		return StageEpisodeFileResult{}, err
	}
	providerSeries, err := providerSeriesForLocal(ctx, *series, opts.ProviderSeries, opts.ProviderResolver)
	if err != nil {
		return StageEpisodeFileResult{}, err
	}
	if err := validateProviderEpisode(providerSeries, opts.Season.Int(), opts.Episode.Int()); err != nil {
		return StageEpisodeFileResult{}, err
	}

	mediaPath, err := cleanAbsoluteFilePath(opts.MediaPath)
	if err != nil {
		return StageEpisodeFileResult{}, err
	}
	if !fsroot.RecognizedVideoFile(mediaPath) {
		return StageEpisodeFileResult{}, fmt.Errorf("episode path %q is not a recognized video file", mediaPath)
	}
	companionPaths := make([]string, 0, len(opts.Companions))
	for _, companion := range opts.Companions {
		companionPath, err := cleanAbsoluteFilePath(companion)
		if err != nil {
			return StageEpisodeFileResult{}, err
		}
		companionPaths = append(companionPaths, companionPath)
	}
	if opts.Inspector == nil {
		return StageEpisodeFileResult{}, errors.New("library: media inspector is required")
	}

	activeExists := episodeExists(*series, opts.Season.Int(), opts.Episode.Int())
	if activeExists && !opts.Replace {
		return StageEpisodeFileResult{}, EpisodeAlreadyExistsError{Season: opts.Season.Int(), Episode: opts.Episode.Int()}
	}
	staged, err := repo.LoadStaged(seriesDir.Path())
	if err != nil {
		return StageEpisodeFileResult{}, err
	}
	updated := *staged
	_, existingIndex, stagedExists := updated.Lookup(opts.Season.Int(), opts.Episode.Int())
	if stagedExists && !opts.Replace {
		return StageEpisodeFileResult{}, StagedEpisodeAlreadyExistsError{Season: opts.Season.Int(), Episode: opts.Episode.Int()}
	}

	progress.Start(ctx, "episode-stage", fmt.Sprintf("Inspecting media: %s", mediaPath), 1)
	mediaInfo, err := opts.Inspector.Inspect(ctx, mediaPath)
	if err != nil {
		progress.Failure(ctx, "episode-stage", fmt.Sprintf("Failed inspecting %s", mediaPath), 1, 1)
		return StageEpisodeFileResult{}, err
	}
	source := opts.Source
	if source == "" {
		source = fsroot.InferSourceFromFilename(mediaPath)
	}
	entry, err := stagedEpisode(opts.Season.Int(), opts.Episode.Int(), mediaPath, source.String(), companionPaths, &mediaInfo)
	if err != nil {
		return StageEpisodeFileResult{}, err
	}
	if stagedExists {
		updated.Entries[existingIndex] = entry
	} else {
		updated.Entries = append(updated.Entries, entry)
	}
	if err := updated.Validate(); err != nil {
		return StageEpisodeFileResult{}, err
	}

	if opts.Apply {
		progress.Update(ctx, "episode-stage", fmt.Sprintf("Writing staged metadata: %s", store.StagedPath(seriesDir.Path())), 1, 1)
		if err := repo.SaveStaged(updated); err != nil {
			progress.Failure(ctx, "episode-stage", "Failed writing staged metadata", 1, 1)
			return StageEpisodeFileResult{}, err
		}
	}
	progress.Success(ctx, "episode-stage", fmt.Sprintf("Staged episode media: %s", mediaPath), 1)
	return StageEpisodeFileResult{
		Series:        seriesDir.Name(),
		DryRun:        !opts.Apply,
		Replaced:      activeExists || stagedExists,
		Entry:         entry,
		UpdatedStaged: updated,
	}, nil
}

func stagedEpisode(season int, number int, mediaPath string, source string, companions []string, mediaInfo *domain.MediaInfo) (store.StagedEpisode, error) {
	info, err := os.Stat(mediaPath)
	if err != nil {
		return store.StagedEpisode{}, err
	}
	if info.IsDir() {
		return store.StagedEpisode{}, fmt.Errorf("library: episode path %q is a directory", mediaPath)
	}
	companionFiles, err := absoluteCompanionFiles(companions)
	if err != nil {
		return store.StagedEpisode{}, err
	}
	return store.StagedEpisode{
		Season: season,
		Number: number,
		Episode: store.Episode{
			Number: number,
			Media: store.MediaFile{
				Path:      mediaPath,
				Source:    domain.ParseMediaSource(source).String(),
				Size:      info.Size(),
				MTime:     info.ModTime().UTC().Format(time.RFC3339),
				MediaInfo: mediaInfo,
			},
			Companions: companionFiles,
		},
	}, nil
}

func absoluteCompanionFiles(paths []string) ([]store.CompanionFile, error) {
	if len(paths) == 0 {
		return []store.CompanionFile{}, nil
	}
	out := make([]store.CompanionFile, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("library: companion path %q is a directory", path)
		}
		out = append(out, store.CompanionFile{
			Path:  path,
			Size:  info.Size(),
			MTime: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
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
	return path, nil
}

func resolveSeriesForWorkflow(root fsroot.LibraryRoot, series string) (fsroot.SeriesDir, error) {
	return root.SeriesDir(series)
}

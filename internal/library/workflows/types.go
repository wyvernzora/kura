package workflows

import (
	"context"

	media "github.com/wyvernzora/kura/internal/domain"
	layout "github.com/wyvernzora/kura/internal/fsroot"
	scan "github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/library/models"
	"github.com/wyvernzora/kura/internal/metadata"
)

type LibraryRoot = layout.LibraryRoot
type SeriesDir = layout.SeriesDir
type SeasonNumber = media.SeasonNumber
type EpisodeNumber = media.EpisodeNumber
type MediaSource = media.MediaSource
type MediaInfo = media.MediaInfo

type Series = models.Series
type Season = models.Season
type Episode = models.Episode
type Staged = models.Staged
type StagedEpisode = models.StagedEpisode
type Trash = models.Trash
type TrashedEpisode = models.TrashedEpisode
type MediaFile = models.MediaFile
type CompanionFile = models.CompanionFile

type DiscoveredEpisode = scan.DiscoveredEpisode
type ImportSkip = scan.ImportSkip

type MediaInspector interface {
	Inspect(context.Context, string) (MediaInfo, error)
}

type MediaInspectorFunc func(context.Context, string) (MediaInfo, error)

func (f MediaInspectorFunc) Inspect(ctx context.Context, path string) (MediaInfo, error) {
	return f(ctx, path)
}

type ProviderSeriesResolver func(context.Context, Series) (metadata.Series, error)

var (
	CleanFilesystemTitle    = media.CleanFilesystemTitle
	ParseMediaSource        = media.ParseMediaSource
	DiscoverSeriesEpisodes  = scan.DiscoverSeriesEpisodes
	InferSourceFromFilename = scan.InferSourceFromFilename
	RecognizedVideoFile     = scan.RecognizedVideoFile
)

func SeriesPath(seriesDir string) string {
	return models.SeriesPath(seriesDir)
}

func StagedPath(seriesDir string) string {
	return models.StagedPath(seriesDir)
}

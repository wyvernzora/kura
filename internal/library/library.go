package library

import (
	"context"

	"github.com/wyvernzora/kura/internal/library/layout"
	"github.com/wyvernzora/kura/internal/library/match"
	"github.com/wyvernzora/kura/internal/library/media"
	"github.com/wyvernzora/kura/internal/library/reconcile"
	"github.com/wyvernzora/kura/internal/library/scan"
	"github.com/wyvernzora/kura/internal/library/series"
	"github.com/wyvernzora/kura/internal/progress"
)

type LibraryRoot = layout.LibraryRoot
type SeriesDir = layout.SeriesDir
type FilesystemTitle = layout.FilesystemTitle
type SeasonNumber = layout.SeasonNumber
type EpisodeNumber = layout.EpisodeNumber
type EpisodeRef = layout.EpisodeRef
type MediaFilename = layout.MediaFilename
type MediaFilenameFacts = layout.MediaFilenameFacts

type MediaSource = media.MediaSource
type Codec = media.Codec
type Resolution = media.Resolution
type MediaInfo = media.MediaInfo

type Series = series.Series
type Season = series.Season
type Episode = series.Episode
type TrashedEpisode = series.TrashedEpisode
type MediaFile = series.MediaFile
type CompanionFile = series.CompanionFile
type AddEpisodeOptions = series.AddEpisodeOptions
type DuplicateEpisodeNumberError = series.DuplicateEpisodeNumberError
type EpisodeAlreadyExistsError = series.EpisodeAlreadyExistsError

type DiscoveredEpisode = scan.DiscoveredEpisode
type ImportSkip = scan.ImportSkip

type ResolveSeriesOptions = match.ResolveSeriesOptions
type SeriesSelectionRequiredError = match.SeriesSelectionRequiredError

type ReconcilePlan = reconcile.Plan
type ReconcileMove = reconcile.Move

type ProgressStatus = progress.Status
type ProgressEvent = progress.Event
type ProgressReporter = progress.Reporter

const (
	SeriesSchemaVersion = series.SeriesSchemaVersion

	MediaSourceUnknown = media.MediaSourceUnknown
	MediaSourceTVRip   = media.MediaSourceTVRip
	MediaSourceWebRip  = media.MediaSourceWebRip
	MediaSourceWebDL   = media.MediaSourceWebDL
	MediaSourceBDRip   = media.MediaSourceBDRip
	MediaSourceBluRay  = media.MediaSourceBluRay
	MediaSourceHDTV    = media.MediaSourceHDTV
	MediaSourceDVDRip  = media.MediaSourceDVDRip

	ProgressStart   = progress.StartStatus
	ProgressUpdate  = progress.UpdateStatus
	ProgressSuccess = progress.SuccessStatus
	ProgressFailure = progress.FailureStatus
)

var (
	ParseLibraryRoot     = layout.ParseLibraryRoot
	ParseSeriesDir       = layout.ParseSeriesDir
	ParseFilesystemTitle = layout.ParseFilesystemTitle
	CleanFilesystemTitle = layout.CleanFilesystemTitle
	NewSeasonNumber      = layout.NewSeasonNumber
	ParseSeasonNumber    = layout.ParseSeasonNumber
	RegularSeason        = layout.RegularSeason
	SpecialsSeason       = layout.SpecialsSeason
	NewEpisodeNumber     = layout.NewEpisodeNumber
	ParseEpisodeNumber   = layout.ParseEpisodeNumber
	NewEpisodeRef        = layout.NewEpisodeRef
	BuildMediaFilename   = layout.BuildMediaFilename

	ParseMediaSource = media.ParseMediaSource
	ParseCodec       = media.ParseCodec
	NewResolution    = media.NewResolution
	ParseResolution  = media.ParseResolution

	AddEpisode = series.AddEpisode

	DiscoverSeriesEpisodes   = scan.DiscoverSeriesEpisodes
	ParseSeasonDir           = scan.ParseSeasonDir
	InferEpisodeFromFilename = scan.InferEpisodeFromFilename
	InferSourceFromFilename  = scan.InferSourceFromFilename
	RecognizedVideoFile      = scan.RecognizedVideoFile

	ResolveProviderSeries  = match.ResolveProviderSeries
	GetProviderSeriesByRef = match.GetProviderSeriesByRef
	SearchResultMatch      = match.SearchResultMatch
	ExactSearchMatch       = match.ExactSearchMatch
	ExactTitleMatch        = match.ExactTitleMatch
	TitleContainsQuery     = match.TitleContainsQuery
)

func SeriesPath(seriesDir string) string {
	return series.SeriesPath(seriesDir)
}

func WithProgress(ctx context.Context, reporter ProgressReporter) context.Context {
	return progress.With(ctx, reporter)
}

// Library owns filesystem-backed series metadata operations.
type Library interface {
	NewSeries(dirname string) (*Series, error)
	LoadSeries(dirname string) (*Series, error)
	SaveSeries(series Series) error
	SyncSeries(ctx context.Context, root LibraryRoot, dirname string, opts SeriesSyncOptions) (SeriesSyncResult, error)
	ImportEpisodeFile(ctx context.Context, root LibraryRoot, opts ImportEpisodeFileOptions) (Series, error)
	PlanReconcile(ctx context.Context, root LibraryRoot, dirname string) (ReconcilePlan, error)
	ApplyReconcile(ctx context.Context, plan ReconcilePlan) error
}

type library struct {
	store series.Store
}

var _ Library = library{}

// New returns the default filesystem-backed library implementation.
func New() Library {
	return library{store: series.NewStore()}
}

func (l library) NewSeries(dirname string) (*Series, error) {
	return l.store.New(dirname)
}

func (l library) LoadSeries(dirname string) (*Series, error) {
	return l.store.Load(dirname)
}

func (l library) SaveSeries(series Series) error {
	return l.store.Save(series)
}

func (l library) PlanReconcile(ctx context.Context, root LibraryRoot, dirname string) (ReconcilePlan, error) {
	return reconcile.PlanSeries(ctx, root, dirname, l.store)
}

func (l library) ApplyReconcile(ctx context.Context, plan ReconcilePlan) error {
	return reconcile.ApplyPlan(ctx, plan, l.store)
}

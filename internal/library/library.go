package library

import (
	"context"

	"github.com/wyvernzora/kura/internal/domain"
	layout "github.com/wyvernzora/kura/internal/fsroot"
	scan "github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/library/reconcile"
	"github.com/wyvernzora/kura/internal/library/workflows"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/resolve"
	"github.com/wyvernzora/kura/internal/store"
)

type LibraryRoot = layout.LibraryRoot
type SeriesDir = layout.SeriesDir
type FilesystemTitle = domain.FilesystemTitle
type SeasonNumber = domain.SeasonNumber
type EpisodeNumber = domain.EpisodeNumber
type EpisodeRef = domain.EpisodeRef
type MediaFilename = domain.MediaFilename
type MediaFilenameFacts = domain.MediaFilenameFacts

type MediaSource = domain.MediaSource
type Codec = domain.Codec
type Resolution = domain.Resolution
type MediaInfo = domain.MediaInfo

type Series = store.Series
type Season = store.Season
type Episode = store.Episode
type Staged = store.Staged
type StagedEpisode = store.StagedEpisode
type Trash = store.Trash
type TrashedEpisode = store.TrashedEpisode
type MediaFile = store.MediaFile
type CompanionFile = store.CompanionFile
type DuplicateEpisodeNumberError = store.DuplicateEpisodeNumberError

type AddEpisodeOptions = workflows.AddEpisodeOptions
type EpisodeAlreadyExistsError = workflows.EpisodeAlreadyExistsError

type DiscoveredEpisode = scan.DiscoveredEpisode
type ImportSkip = scan.ImportSkip

type MediaInspector = workflows.MediaInspector
type MediaInspectorFunc = workflows.MediaInspectorFunc
type ProviderSeriesResolver = workflows.ProviderSeriesResolver
type SeriesSyncOptions = workflows.SeriesSyncOptions
type SeriesSyncResult = workflows.SeriesSyncResult
type SeriesSyncEntry = workflows.SeriesSyncEntry
type StageEpisodeFileOptions = workflows.StageEpisodeFileOptions
type StageEpisodeFileResult = workflows.StageEpisodeFileResult
type StagedEpisodeAlreadyExistsError = workflows.StagedEpisodeAlreadyExistsError

type ResolveSeriesOptions = resolve.ResolveSeriesOptions
type SeriesSelectionRequiredError = resolve.SeriesSelectionRequiredError

type ReconcilePlan = reconcile.Plan
type ReconcileMove = reconcile.Move

type ProgressStatus = progress.Status
type ProgressEvent = progress.Event
type ProgressReporter = progress.Reporter

const (
	SeriesSchemaVersion = store.SeriesSchemaVersion
	StagedSchemaVersion = store.StagedSchemaVersion
	TrashSchemaVersion  = store.TrashSchemaVersion

	MediaSourceUnknown = domain.MediaSourceUnknown
	MediaSourceTVRip   = domain.MediaSourceTVRip
	MediaSourceWebRip  = domain.MediaSourceWebRip
	MediaSourceWebDL   = domain.MediaSourceWebDL
	MediaSourceBDRip   = domain.MediaSourceBDRip
	MediaSourceBluRay  = domain.MediaSourceBluRay
	MediaSourceHDTV    = domain.MediaSourceHDTV
	MediaSourceDVDRip  = domain.MediaSourceDVDRip

	ProgressStart   = progress.StartStatus
	ProgressUpdate  = progress.UpdateStatus
	ProgressSuccess = progress.SuccessStatus
	ProgressFailure = progress.FailureStatus
)

var (
	ParseLibraryRoot     = layout.ParseLibraryRoot
	ParseSeriesDir       = layout.ParseSeriesDir
	ParseFilesystemTitle = domain.ParseFilesystemTitle
	CleanFilesystemTitle = domain.CleanFilesystemTitle
	NewSeasonNumber      = domain.NewSeasonNumber
	ParseSeasonNumber    = domain.ParseSeasonNumber
	RegularSeason        = domain.RegularSeason
	SpecialsSeason       = domain.SpecialsSeason
	NewEpisodeNumber     = domain.NewEpisodeNumber
	ParseEpisodeNumber   = domain.ParseEpisodeNumber
	NewEpisodeRef        = domain.NewEpisodeRef
	BuildMediaFilename   = domain.BuildMediaFilename

	ParseMediaSource = domain.ParseMediaSource
	ParseCodec       = domain.ParseCodec
	NewResolution    = domain.NewResolution
	ParseResolution  = domain.ParseResolution

	DiscoverSeriesEpisodes   = scan.DiscoverSeriesEpisodes
	ParseSeasonDir           = scan.ParseSeasonDir
	InferEpisodeFromFilename = scan.InferEpisodeFromFilename
	InferSourceFromFilename  = scan.InferSourceFromFilename
	RecognizedVideoFile      = scan.RecognizedVideoFile

	ResolveProviderSeries  = resolve.ResolveProviderSeries
	GetProviderSeriesByRef = resolve.GetProviderSeriesByRef
	SearchResultMatch      = resolve.SearchResultMatch
	ExactSearchMatch       = resolve.ExactSearchMatch
	ExactTitleMatch        = resolve.ExactTitleMatch
	TitleContainsQuery     = resolve.TitleContainsQuery
)

func SeriesPath(seriesDir string) string {
	return store.SeriesPath(seriesDir)
}

func StagedPath(seriesDir string) string {
	return store.StagedPath(seriesDir)
}

func WithProgress(ctx context.Context, reporter ProgressReporter) context.Context {
	return progress.With(ctx, reporter)
}

// Library owns filesystem-backed series metadata operations.
type Library interface {
	NewSeries(dirname string) (*Series, error)
	LoadSeries(dirname string) (*Series, error)
	SaveSeries(series Series) error
	LoadStaged(dirname string) (*Staged, error)
	SaveStaged(staged Staged) error
	LoadTrash(dirname string) (*Trash, error)
	SaveTrash(trash Trash) error
	SyncSeries(ctx context.Context, root LibraryRoot, dirname string, opts SeriesSyncOptions) (SeriesSyncResult, error)
	StageEpisodeFile(ctx context.Context, root LibraryRoot, dirname string, opts StageEpisodeFileOptions) (StageEpisodeFileResult, error)
	PlanReconcile(ctx context.Context, root LibraryRoot, dirname string) (ReconcilePlan, error)
	ApplyReconcile(ctx context.Context, plan ReconcilePlan) error
}

type library struct {
	store store.Repo
}

var _ Library = library{}

// New returns the default filesystem-backed library implementation.
func New() Library {
	return library{store: store.NewRepo()}
}

func (l library) NewSeries(dirname string) (*Series, error) {
	return l.store.NewSeries(dirname)
}

func (l library) LoadSeries(dirname string) (*Series, error) {
	return l.store.LoadSeries(dirname)
}

func (l library) SaveSeries(series Series) error {
	return l.store.SaveSeries(series)
}

func (l library) LoadStaged(dirname string) (*Staged, error) {
	return l.store.LoadStaged(dirname)
}

func (l library) SaveStaged(staged Staged) error {
	return l.store.SaveStaged(staged)
}

func (l library) LoadTrash(dirname string) (*Trash, error) {
	return l.store.LoadTrash(dirname)
}

func (l library) SaveTrash(trash Trash) error {
	return l.store.SaveTrash(trash)
}

func AddEpisode(seriesDir string, series Series, opts AddEpisodeOptions) (Series, error) {
	return workflows.AddEpisode(seriesDir, series, opts)
}

func (l library) SyncSeries(ctx context.Context, root LibraryRoot, dirname string, opts SeriesSyncOptions) (SeriesSyncResult, error) {
	return workflows.SyncSeries(ctx, l.store, root, dirname, opts)
}

func (l library) StageEpisodeFile(ctx context.Context, root LibraryRoot, dirname string, opts StageEpisodeFileOptions) (StageEpisodeFileResult, error) {
	return workflows.StageEpisodeFile(ctx, l.store, root, dirname, opts)
}

func (l library) PlanReconcile(ctx context.Context, root LibraryRoot, dirname string) (ReconcilePlan, error) {
	return reconcile.PlanSeries(ctx, root, dirname, l.store)
}

func (l library) ApplyReconcile(ctx context.Context, plan ReconcilePlan) error {
	return reconcile.ApplyPlan(ctx, plan, l.store)
}

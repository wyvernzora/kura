package series

import (
	"context"
	"errors"

	scanworkflow "github.com/wyvernzora/kura/internal/series/scan"
)

type ScanInput = scanworkflow.Input

type ScanResult = scanworkflow.Result

type ImportSkip = scanworkflow.ImportSkip

const (
	SkipCodeSpecialNumberNotInferred = scanworkflow.SkipCodeSpecialNumberNotInferred
	SkipCodeEpisodeNumberNotInferred = scanworkflow.SkipCodeEpisodeNumberNotInferred
	SkipCodeSeasonMismatch           = scanworkflow.SkipCodeSeasonMismatch
	SkipCodeIgnoredDirectory         = scanworkflow.SkipCodeIgnoredDirectory
)

type ScannedEpisode = scanworkflow.ScannedEpisode

type ScanStatus = scanworkflow.ScanStatus

const (
	ScanStatusAdded     = scanworkflow.ScanStatusAdded
	ScanStatusReplaced  = scanworkflow.ScanStatusReplaced
	ScanStatusUpdated   = scanworkflow.ScanStatusUpdated
	ScanStatusUnchanged = scanworkflow.ScanStatusUnchanged
	ScanStatusRemoved   = scanworkflow.ScanStatusRemoved
)

type EpisodeAlreadyExistsError = scanworkflow.EpisodeAlreadyExistsError

type MetadataMissingEpisodeError = scanworkflow.MetadataMissingEpisodeError

type ScanStagedRecordsError = scanworkflow.ScanStagedRecordsError

func (h Handle) Scan(ctx context.Context, in ScanInput) (ScanResult, error) {
	return scanworkflow.NewRunner(h.root(), h.ref, h.source(), h.inspector(), h.now).Scan(ctx, in)
}

func IsNotTracked(err error) bool {
	var notTracked SeriesNotTrackedError
	return errors.As(err, &notTracked)
}

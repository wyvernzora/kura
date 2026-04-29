package kura

import (
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/internal/resolve"
)

var (
	ErrRootNotFound     = errors.New("kura: library root does not exist")
	ErrRootNotDirectory = errors.New("kura: library root is not a directory")
	ErrMissingTVDBKey   = errors.New("kura: tvdb api key is required")
)

var (
	ErrEmptyQuery       = resolve.ErrEmptyQuery
	ErrTooManyTerms     = resolve.ErrTooManyTerms
	ErrConflictingTerms = resolve.ErrConflictingTerms
	ErrNoStrategyMatch  = resolve.ErrNoStrategyMatch
	ErrStaleMetadataRef = resolve.ErrStaleMetadataRef
)

type MetadataRefNotIndexedError struct {
	Ref MetadataRef
}

func (err MetadataRefNotIndexedError) Error() string {
	return fmt.Sprintf("kura: metadata ref %s is not indexed; run kura import or kura add", err.Ref)
}

type SeriesAlreadyExistsError struct {
	Ref SeriesRef
}

func (err SeriesAlreadyExistsError) Error() string {
	return fmt.Sprintf("kura: series %q already exists below the library root", err.Ref)
}

type SeriesNotFoundError struct {
	Ref SeriesRef
}

func (err SeriesNotFoundError) Error() string {
	return fmt.Sprintf("kura: series %q does not exist below the library root", err.Ref)
}

type SeriesNotTrackedError struct {
	Ref SeriesRef
}

func (err SeriesNotTrackedError) Error() string {
	return fmt.Sprintf("kura: series %q is not tracked; run kura import or kura add", err.Ref)
}

type SeriesAlreadyTrackedError struct {
	Ref SeriesRef
}

func (err SeriesAlreadyTrackedError) Error() string {
	return fmt.Sprintf("kura: series %q already has .kura/series.json; use kura scan instead", err.Ref)
}

type MetadataRefConflictError struct {
	Ref      MetadataRef
	Existing SeriesRef
	Next     SeriesRef
}

func (err MetadataRefConflictError) Error() string {
	return fmt.Sprintf("kura: metadata ref %s is already tracked at %q", err.Ref, err.Existing)
}

type UnsupportedMetadataSourceError struct {
	Source string
}

func (err UnsupportedMetadataSourceError) Error() string {
	return fmt.Sprintf("kura: unsupported metadata ref source %q; only tvdb:<id> is supported", err.Source)
}

type EpisodeAlreadyTrackedError struct {
	Series   SeriesRef
	Season   int
	Episode  int
	Existing string
	Found    string
}

func (err EpisodeAlreadyTrackedError) Error() string {
	return fmt.Sprintf("kura: %s already tracks S%02dE%02d at %q; pass --replace to replace it", err.Series, err.Season, err.Episode, err.Existing)
}

type StagedEpisodeAlreadyExistsError struct {
	Series  SeriesRef
	Season  int
	Episode int
}

func (err StagedEpisodeAlreadyExistsError) Error() string {
	return fmt.Sprintf("kura: %s already has staged S%02dE%02d; pass --replace to replace it", err.Series, err.Season, err.Episode)
}

type MetadataMissingEpisodeError struct {
	Series  SeriesRef
	Season  int
	Episode int
}

func (err MetadataMissingEpisodeError) Error() string {
	return fmt.Sprintf("kura: metadata for %s has no S%02dE%02d", err.Series, err.Season, err.Episode)
}

type PlanStaleError struct {
	Series SeriesRef
}

func (err PlanStaleError) Error() string {
	return fmt.Sprintf("kura: reconcile plan for %s is stale; re-plan before applying", err.Series)
}

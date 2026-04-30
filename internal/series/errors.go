package series

import (
	"fmt"

	"github.com/wyvernzora/kura/internal/refs"
)

type MetadataRefNotIndexedError struct {
	Ref refs.Metadata
}

func (err MetadataRefNotIndexedError) Error() string {
	return fmt.Sprintf("series: metadata ref %s is not indexed", err.Ref)
}

type SeriesAlreadyExistsError struct {
	Ref refs.Series
}

func (err SeriesAlreadyExistsError) Error() string {
	return fmt.Sprintf("series: %q already exists below the library root", err.Ref)
}

type SeriesNotFoundError struct {
	Ref refs.Series
}

func (err SeriesNotFoundError) Error() string {
	return fmt.Sprintf("series: %q does not exist below the library root", err.Ref)
}

type SeriesNotTrackedError struct {
	Ref refs.Series
}

func (err SeriesNotTrackedError) Error() string {
	return fmt.Sprintf("series: %q is not tracked", err.Ref)
}

type SeriesAlreadyTrackedError struct {
	Ref refs.Series
}

func (err SeriesAlreadyTrackedError) Error() string {
	return fmt.Sprintf("series: %q already has .kura/series.json", err.Ref)
}

type MetadataRefConflictError struct {
	Ref      refs.Metadata
	Existing refs.Series
	Next     refs.Series
}

func (err MetadataRefConflictError) Error() string {
	return fmt.Sprintf("series: metadata ref %s is already tracked at %q", err.Ref, err.Existing)
}

type UnsupportedMetadataSourceError struct {
	Source string
}

func (err UnsupportedMetadataSourceError) Error() string {
	return fmt.Sprintf("series: unsupported metadata ref source %q", err.Source)
}

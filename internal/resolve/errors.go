package resolve

import "errors"

var (
	ErrEmptyQuery        = errors.New("resolve: query has no terms")
	ErrTooManyTerms      = errors.New("resolve: query exceeds maximum term count")
	ErrConflictingTerms  = errors.New("resolve: query has conflicting authoritative terms")
	ErrNoStrategyMatch   = errors.New("resolve: no strategy matches term")
	ErrCorruptSeriesFile = errors.New("resolve: series.json is corrupt")
	ErrStaleMetadataRef  = errors.New("resolve: stored metadata ref is no longer recognized upstream")
)

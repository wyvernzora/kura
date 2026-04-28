// Package metadata defines Kura's external metadata contracts.
//
// Implementations in source-specific packages, such as TVDB, adapt external API
// response shapes into these types. These values represent live metadata facts
// and are not the persistent on-disk schema for .kura/series.json.
package metadata

import (
	"context"
	"errors"
)

// Source retrieves series, season, and episode metadata from an external
// metadata source.
type Source interface {
	// Key returns the stable provider key used in provider refs, such as "tvdb".
	Key() string

	// Search returns lightweight candidate matches for a title query.
	Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)

	// GetSeries returns the complete metadata view Kura needs for a series.
	//
	// Implementations may satisfy this by making more than one upstream request.
	// Callers should treat the returned data as live external metadata suitable for
	// read views, matching, and filesystem title selection, not as durable local
	// library state.
	GetSeries(ctx context.Context, providerID string) (Series, error)
}

// SearchOptions scopes metadata search without making the search interface
// source-specific.
type SearchOptions struct {
	// Limit caps the number of returned candidates. Zero means the source
	// implementation should use a reasonable default.
	Limit int

	// Type restricts results when a source supports mixed media search.
	Type MediaType

	// Year restricts results to an initial release year when supported.
	Year int
}

// SearchResult is a lightweight candidate returned by Source.Search. It is
// intended for matching and disambiguation, not for building full library read
// views.
//
// This includes search-only metadata beyond the shared SeriesSummary to support
// match reporting and ranking in the caller.
type SearchResult struct {
	SeriesSummary

	// Score is the provider-reported relevance score when available.
	Score float64

	// MatchSource identifies the search response field that matched the query,
	// for example "title".
	MatchSource string
}

// SeriesSummary contains series-level metadata shared by search results and
// full series views.
type SeriesSummary struct {
	// ProviderRef is this series' opaque provider reference, such as "tvdb:12345".
	ProviderRef string

	// ProviderRefs contains ProviderRef plus any linked provider references
	// discovered from the metadata source, such as imdb:tt12345.
	ProviderRefs []string

	// PreferredTitle is Kura's selected official title after source normalization and
	// language preference handling.
	PreferredTitle string

	// CanonicalTitle is the source's canonical title for the series.
	CanonicalTitle string

	Type   MediaType
	Status SeriesStatus
	Year   int

	OriginalLanguage string
	OriginalCountry  string
	FirstAired       string

	Genres []string
}

// Series is the source-neutral metadata shape used by Kura read workflows.
// It represents live external metadata and should not be written directly to
// .kura/series.json.
type Series struct {
	SeriesSummary

	LastAired string
	Seasons   []Season
	Specials  *Season
}

// Error sentinels shared across metadata providers.

var (
	ErrNotFound     = errors.New("metadata: not found")
	ErrUnauthorized = errors.New("metadata: unauthorized")
	ErrUnavailable  = errors.New("metadata: unavailable")
)

// Season contains external metadata for one season.
type Season struct {
	// ProviderRef is this season's opaque provider reference.
	ProviderRef string

	Number int

	Episodes []Episode
}

// Episode contains external metadata for one episode.
type Episode struct {
	// ProviderRef is this episode's opaque provider reference.
	ProviderRef string

	SeasonNumber   int
	EpisodeNumber  int
	AbsoluteNumber *int

	// Aired is the provider's YYYY-MM-DD air date when known.
	Aired string
}

// MediaType identifies the broad metadata entity type.
type MediaType string

const (
	MediaTypeUnknown MediaType = ""
	MediaTypeSeries  MediaType = "series"
	MediaTypeMovie   MediaType = "movie"
)

// SeriesStatus is a normalized metadata status.
type SeriesStatus string

const (
	SeriesStatusUnknown    SeriesStatus = ""
	SeriesStatusUpcoming   SeriesStatus = "upcoming"
	SeriesStatusContinuing SeriesStatus = "continuing"
	SeriesStatusEnded      SeriesStatus = "ended"
)

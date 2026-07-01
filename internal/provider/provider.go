// Package provider defines Kura's external metadata-source contracts.
//
// Implementations in source-specific packages, such as TVDB, adapt external API
// response shapes into these types. These values represent live metadata facts
// and are not the persistent on-disk schema for .kura/series.json.
package provider

import (
	"context"
	"errors"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/textnorm"
)

// Source retrieves series, season, and episode metadata from an external
// metadata source.
type Source interface {
	// Key returns the stable source key used in metadata refs, such as "tvdb".
	Key() string

	// Search returns lightweight candidate matches for a title query.
	Search(ctx context.Context, query textnorm.NFCString, opts SearchOptions) ([]SearchResult, error)

	// GetSeries returns the complete metadata view Kura needs for a series.
	//
	// ordering selects the episode spine ordering to fetch (e.g. "default",
	// "dvd", "absolute", "alternate", "official", "regional"). Empty string
	// asks the source for its default ordering.
	//
	// Implementations may satisfy this by making more than one upstream request.
	// Callers should treat the returned data as live external metadata suitable for
	// read views, matching, and filesystem title selection, not as durable local
	// library state.
	GetSeries(ctx context.Context, metadataID, ordering string) (Series, error)
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

	// Score is the source-reported relevance score when available.
	Score float64

	// MatchSource identifies the search response field that matched the query,
	// for example "title".
	MatchSource string

	// Aliases are source-provided alternate titles usable for resolver evidence.
	Aliases []textnorm.NFCString
}

// SeriesSummary contains series-level metadata shared by search results and
// full series views.
type SeriesSummary struct {
	// MetadataRef is this series' opaque metadata reference, such as "tvdb:12345".
	MetadataRef refs.Metadata

	// PreferredTitle is Kura's selected official title after source normalization and
	// language preference handling.
	PreferredTitle textnorm.NFCString

	// CanonicalTitle is the source's canonical title for the series.
	CanonicalTitle textnorm.NFCString

	Type   MediaType
	Status SeriesStatus
	Year   int

	OriginalLanguage string
	OriginalCountry  string
	FirstAired       string

	Genres []string

	// Poster is a lightweight series-level poster reference suitable for
	// candidate lists. For search results it comes straight from the
	// search response (no extra request per candidate); for full series
	// views it mirrors the selected Series.Poster.
	Poster Artwork
}

// Series is the source-neutral metadata shape used by Kura read workflows.
// It represents live external metadata and should not be written directly to
// .kura/series.json.
type Series struct {
	SeriesSummary

	LastAired string
	Seasons   []Season

	// TranslatedTitles is every per-language title the source shipped
	// for this series. Language is normalized to BCP-47 base form
	// (e.g. "ja", "en"); empty Language is permitted for entries
	// without a tag. Used by kura to compose its searchKey fold and
	// is intentionally not surfaced on read APIs.
	TranslatedTitles []TitleEntry

	// Aliases is the source-provided alternate-title list. Same
	// language normalization as TranslatedTitles; empty Language
	// permitted. Like TranslatedTitles, fed into searchKey only —
	// never returned on the wire.
	Aliases []TitleEntry

	// Poster is the selected series-level artwork URL. URL-only;
	// kura does not cache image bytes locally. Empty when no poster
	// matches the caller's preferred-language preference and no
	// fallback artwork is available.
	Poster Artwork
}

// TitleEntry pairs a title string with its language tag. Language is
// BCP-47 base form (e.g. "ja", "en", "zh-Hans"); empty for entries
// the source carries without a language hint (typical for top-level
// alias lists).
type TitleEntry struct {
	Language string
	Value    string
}

// Artwork is a URL-only reference to provider-hosted imagery.
type Artwork struct {
	URL          string
	ThumbnailURL string
	Language     string
}

// Error sentinels shared across metadata sources.

var (
	ErrNotFound     = errors.New("metadata: not found")
	ErrUnauthorized = errors.New("metadata: unauthorized")
	ErrUnavailable  = errors.New("metadata: unavailable")
)

// Season contains external metadata for one season.
type Season struct {
	// MetadataRef is this season's opaque metadata reference.
	MetadataRef refs.Metadata

	Number int

	Episodes []Episode
}

// Episode contains external metadata for one episode.
type Episode struct {
	// MetadataRef is this episode's opaque metadata reference.
	MetadataRef refs.Metadata

	Ref            refs.Episode
	AbsoluteNumber *int

	// Aired is the source's YYYY-MM-DD air date when known.
	Aired string

	// CanonicalTitle is the provider's default-language episode title.
	// PreferredTitle is the title in the caller's first preferred
	// language for which the provider has a translation; empty when
	// no translation exists (caller applies fallback).
	CanonicalTitle textnorm.NFCString
	PreferredTitle textnorm.NFCString
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

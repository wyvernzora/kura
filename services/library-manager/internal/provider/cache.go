package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/wyvernzora/kura/services/library-manager/internal/textnorm"
)

// CacheOptions configures NewCachedSource.
type CacheOptions struct {
	// TTL is how long successful metadata responses remain cached. Zero uses
	// DefaultCacheTTL.
	TTL time.Duration

	// MaxEntries caps the number of cached metadata responses. Zero uses
	// DefaultCacheMaxEntries.
	MaxEntries int
}

// DefaultCacheTTL is the initial metadata response TTL described by the spec.
const DefaultCacheTTL = 10 * time.Minute

// DefaultCacheMaxEntries is the default LRU capacity for metadata responses.
const DefaultCacheMaxEntries = 256

// NewCachedSource wraps a Source with a small in-process TTL cache.
//
// Only successful Search and GetSeries responses are cached. Stored
// values are deep-copied at insertion time, then shared across all
// subsequent calls without further cloning. Callers must treat the
// returned []SearchResult and Series as immutable — mutating a slice
// element or nested map would corrupt the cache for every future
// caller. This is the documented contract for any consumer of the
// wrapped Source, not just the cache layer.
func NewCachedSource(next Source, opts CacheOptions) (Source, error) {
	if next == nil {
		return nil, errors.New("metadata cache: nil source")
	}
	if opts.TTL < 0 {
		return nil, errors.New("metadata cache: negative ttl")
	}
	if opts.TTL == 0 {
		opts.TTL = DefaultCacheTTL
	}
	if opts.MaxEntries < 0 {
		return nil, errors.New("metadata cache: negative max entries")
	}
	if opts.MaxEntries == 0 {
		opts.MaxEntries = DefaultCacheMaxEntries
	}

	return &cachedSource{
		next:    next,
		entries: expirable.NewLRU[string, any](opts.MaxEntries, nil, opts.TTL),
	}, nil
}

type cachedSource struct {
	next    Source
	entries *expirable.LRU[string, any]
}

func (p *cachedSource) Key() string {
	return p.next.Key()
}

func (p *cachedSource) Search(ctx context.Context, query textnorm.NFCString, opts SearchOptions) ([]SearchResult, error) {
	key, err := cacheKey(p.next.Key(), "search", query, opts)
	if err != nil {
		return nil, err
	}
	if cached, ok := p.get(key); ok {
		return cached.([]SearchResult), nil
	}

	results, err := p.next.Search(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	// Store an isolated copy so the upstream Source can safely free or
	// mutate its own buffers; subsequent reads return this copy
	// unchanged. Per Source contract, callers must not mutate.
	stored := cloneSearchResults(results)
	p.set(key, stored)
	return stored, nil
}

func (p *cachedSource) GetSeries(ctx context.Context, metadataID, ordering string) (Series, error) {
	key, err := cacheKey(p.next.Key(), "series", metadataID, ordering)
	if err != nil {
		return Series{}, err
	}
	if cached, ok := p.get(key); ok {
		return cached.(Series), nil
	}

	series, err := p.next.GetSeries(ctx, metadataID, ordering)
	if err != nil {
		return Series{}, err
	}
	// Store an isolated copy so the upstream Source can safely free or
	// mutate its own buffers; subsequent reads return this copy
	// unchanged. Per Source contract, callers must not mutate.
	stored := cloneSeries(series)
	p.set(key, stored)
	return stored, nil
}

func (p *cachedSource) get(key string) (any, bool) {
	return p.entries.Get(key)
}

func (p *cachedSource) set(key string, value any) {
	p.entries.Add(key, value)
}

func cacheKey(sourceKey, method string, parts ...any) (string, error) {
	// JSON gives a stable enough representation for these option structs while
	// keeping the wrapper independent from source-specific key formats.
	body, err := json.Marshal(parts)
	if err != nil {
		return "", fmt.Errorf("metadata cache: build key: %w", err)
	}
	return sourceKey + ":" + method + ":" + string(body), nil
}

func cloneSearchResults(in []SearchResult) []SearchResult {
	if in == nil {
		return nil
	}
	out := make([]SearchResult, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].SeriesSummary = cloneSeriesSummary(in[i].SeriesSummary)
		out[i].Aliases = cloneNFCStrings(in[i].Aliases)
	}
	return out
}

func cloneSeries(in Series) Series {
	out := in
	out.SeriesSummary = cloneSeriesSummary(in.SeriesSummary)
	out.Seasons = cloneSeasons(in.Seasons)
	out.TranslatedTitles = cloneTitleEntries(in.TranslatedTitles)
	out.Aliases = cloneTitleEntries(in.Aliases)
	return out
}

func cloneTitleEntries(in []TitleEntry) []TitleEntry {
	if in == nil {
		return nil
	}
	out := make([]TitleEntry, len(in))
	copy(out, in)
	return out
}

func cloneSeason(in Season) Season {
	out := in
	out.Episodes = cloneEpisodes(in.Episodes)
	return out
}

func cloneSeriesSummary(in SeriesSummary) SeriesSummary {
	out := in
	out.Genres = cloneStrings(in.Genres)
	return out
}

func cloneSeasons(in []Season) []Season {
	if in == nil {
		return nil
	}
	out := make([]Season, len(in))
	for i := range in {
		out[i] = cloneSeason(in[i])
	}
	return out
}

func cloneEpisodes(in []Episode) []Episode {
	if in == nil {
		return nil
	}
	out := make([]Episode, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].AbsoluteNumber = cloneInt(in[i].AbsoluteNumber)
	}
	return out
}

func cloneInt(in *int) *int {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	return append([]string(nil), in...)
}

func cloneNFCStrings(in []textnorm.NFCString) []textnorm.NFCString {
	if len(in) == 0 {
		return []textnorm.NFCString{}
	}
	return append([]textnorm.NFCString(nil), in...)
}

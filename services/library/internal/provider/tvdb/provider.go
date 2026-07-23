// Package tvdb implements Kura's metadata source interface using TVDB API v4.
//
// The package owns TVDB-specific HTTP, auth, pagination, and response
// normalization. Callers should depend on the provider.Source interface unless
// they need TVDB construction options or TVDB-specific error inspection.
package tvdb

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/wyvernzora/kura/services/library/internal/provider"
	"github.com/wyvernzora/kura/services/library/internal/textnorm"
)

const (
	providerKey         = "tvdb"
	defaultURL          = "https://api4.thetvdb.com/v4"
	defaultHTTPTimeout  = 30 * time.Second
	defaultRefreshAhead = 24 * time.Hour
)

// Options configures a TVDB metadata source.
type Options struct {
	// BaseURL overrides the TVDB API base URL. Empty uses the public v4 API.
	BaseURL string

	// HTTPClient overrides the HTTP client. Empty uses a bounded default client.
	HTTPClient *http.Client

	// TokenRefreshBefore controls how early bearer tokens are refreshed.
	// Zero uses a conservative default.
	TokenRefreshBefore time.Duration

	// PreferredLanguages is an ordered allow-list of translated title languages
	// to include alongside the canonical title.
	PreferredLanguages []string
}

// Provider adapts TVDB API responses into source-neutral metadata.
type Provider struct {
	client             *client
	preferredLanguages []string
}

// New creates a TVDB metadata source. The returned provider performs TVDB
// login lazily on the first authenticated request.
func New(apiKey string, opts Options) (*Provider, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("tvdb: missing API key")
	}

	return &Provider{
		client:             newClient(apiKey, opts),
		preferredLanguages: normalizeLanguages(opts.PreferredLanguages),
	}, nil
}

// Key returns the stable provider key used in Kura metadata refs.
func (p *Provider) Key() string {
	return providerKey
}

// Search returns lightweight TVDB series candidates for a title query.
func (p *Provider) Search(ctx context.Context, query textnorm.NFCString, opts provider.SearchOptions) ([]provider.SearchResult, error) {
	if query.IsZero() {
		return nil, errors.New("tvdb: empty search query")
	}
	if opts.Type != "" && opts.Type != provider.MediaTypeSeries {
		// Kura's P0 TVDB integration only searches series. Returning no matches
		// keeps mixed-provider callers from treating unsupported types as errors.
		return nil, nil
	}

	records, err := p.client.search(ctx, query.String(), opts)
	if err != nil {
		return nil, err
	}

	results := make([]provider.SearchResult, 0, len(records))
	for _, record := range records {
		if !isSeriesRecord(record.Type) {
			continue
		}
		results = append(results, p.normalizeSearchResult(record))
	}
	return results, nil
}

// GetSeries returns the complete source-neutral series view Kura needs.
// ordering selects the spine ordering ("default", "dvd", "absolute",
// "alternate", "official", "regional"). Empty applies TVDB's default.
func (p *Provider) GetSeries(ctx context.Context, metadataID, ordering string) (provider.Series, error) {
	metadataID = strings.TrimSpace(metadataID)
	if metadataID == "" {
		return provider.Series{}, errors.New("tvdb: empty series id")
	}

	extended, err := p.client.seriesExtended(ctx, metadataID)
	if err != nil {
		return provider.Series{}, err
	}

	episodes, err := p.client.seriesEpisodes(ctx, metadataID, ordering)
	if err != nil {
		return provider.Series{}, err
	}

	// Optionally fetch the same spine in the operator's first
	// preferred language so we can merge per-episode preferred
	// titles. One extra request per series scan; only fired when
	// the language is non-empty AND differs from the series's
	// origin language (the language episode names from the default
	// call are already in).
	preferredLang := p.firstPreferredLanguage()
	originLang := normalizeLanguage(extended.OriginalLanguage)
	preferredByID := map[int]string{}
	if preferredLang != "" && preferredLang != originLang {
		translated, err := p.client.seriesEpisodesInLanguage(ctx, metadataID, ordering, preferredLang)
		if err != nil {
			// Translation pass is best-effort: a missing translation
			// for the requested language must not fail the scan.
			// Other errors (network / auth) propagate.
			if !errors.Is(err, provider.ErrNotFound) {
				return provider.Series{}, err
			}
		}
		for _, ep := range translated {
			if ep.Name != "" {
				preferredByID[ep.ID] = ep.Name
			}
		}
	}

	return p.normalizeSeries(extended, episodes, preferredByID), nil
}

// firstPreferredLanguage returns the first non-empty entry in
// preferredLanguages, normalized. Empty when no preference is set.
func (p *Provider) firstPreferredLanguage() string {
	for _, lang := range p.preferredLanguages {
		if normalized := normalizeLanguage(lang); normalized != "" {
			return normalized
		}
	}
	return ""
}

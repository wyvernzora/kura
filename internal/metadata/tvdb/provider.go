// Package tvdb implements Kura's metadata source interface using TVDB API v4.
//
// The package owns TVDB-specific HTTP, auth, pagination, and response
// normalization. Callers should depend on the metadata.Source interface unless
// they need TVDB construction options or TVDB-specific error inspection.
package tvdb

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/wyvernzora/kura/internal/metadata"
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
func (p *Provider) Search(ctx context.Context, query string, opts metadata.SearchOptions) ([]metadata.SearchResult, error) {
	query = norm.NFC.String(strings.TrimSpace(query))
	if query == "" {
		return nil, errors.New("tvdb: empty search query")
	}
	if opts.Type != "" && opts.Type != metadata.MediaTypeSeries {
		// Kura's P0 TVDB integration only searches series. Returning no matches
		// keeps mixed-provider callers from treating unsupported types as errors.
		return nil, nil
	}

	records, err := p.client.search(ctx, query, opts)
	if err != nil {
		return nil, err
	}

	results := make([]metadata.SearchResult, 0, len(records))
	for _, record := range records {
		if !isSeriesRecord(record.Type) {
			continue
		}
		results = append(results, p.normalizeSearchResult(record))
	}
	return results, nil
}

// GetSeries returns the complete source-neutral series view Kura needs.
func (p *Provider) GetSeries(ctx context.Context, metadataID string) (metadata.Series, error) {
	metadataID = strings.TrimSpace(metadataID)
	if metadataID == "" {
		return metadata.Series{}, errors.New("tvdb: empty series id")
	}

	extended, err := p.client.seriesExtended(ctx, metadataID)
	if err != nil {
		return metadata.Series{}, err
	}

	episodes, err := p.client.seriesEpisodes(ctx, metadataID)
	if err != nil {
		return metadata.Series{}, err
	}

	return p.normalizeSeries(extended, episodes), nil
}

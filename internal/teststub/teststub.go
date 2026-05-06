//go:build e2e_stub

// Package teststub provides in-memory provider + inspector stubs that
// kura serve wires in when launched with --provider-stub /
// --inspector-stub. Build-tagged e2e_stub so the production binary
// never references this package.
//
// Used by:
//   - e2e harness (subprocess kura-e2e binary)
//   - manual local testing without TVDB credentials or mediainfo
package teststub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/textnorm"
)

// FixtureProviderKey is the metadata-ref key under which fixture
// series are addressable. Selectors look like "stub:<id>".
const FixtureProviderKey = "stub"

// Provider implements provider.Source against an in-memory fixture
// map keyed by series ID. Search returns nothing; resolution flows
// must use direct stub:<id> refs.
type Provider struct {
	seriesByID map[string]provider.Series
}

// NewDefaultProvider returns a Provider seeded with one canonical
// series: stub:1001, "Stub Show", 1 season, 3 episodes. Mirrors the
// fixture historically used by e2e/stub_provider_test.go.
func NewDefaultProvider() *Provider {
	ep1, _ := refs.NewEpisode(1, 1)
	ep2, _ := refs.NewEpisode(1, 2)
	ep3, _ := refs.NewEpisode(1, 3)
	return &Provider{
		seriesByID: map[string]provider.Series{
			"1001": {
				SeriesSummary: provider.SeriesSummary{
					MetadataRef:    "stub:1001",
					PreferredTitle: textnorm.NFC("Stub Show"),
					CanonicalTitle: textnorm.NFC("Stub Show"),
					Status:         provider.SeriesStatusContinuing,
				},
				Seasons: []provider.Season{
					{
						Number: 1,
						Episodes: []provider.Episode{
							{
								Ref:            ep1,
								Aired:          "2020-01-01",
								PreferredTitle: textnorm.NFC("Pilot"),
								CanonicalTitle: textnorm.NFC("Pilot"),
							},
							{
								Ref:            ep2,
								Aired:          "2020-01-08",
								PreferredTitle: textnorm.NFC("Episode 2"),
								CanonicalTitle: textnorm.NFC("Episode 2"),
							},
							{
								Ref:            ep3,
								Aired:          "2020-01-15",
								PreferredTitle: textnorm.NFC("Episode 3"),
								CanonicalTitle: textnorm.NFC("Episode 3"),
							},
						},
					},
				},
			},
		},
	}
}

// LoadProvider parses a fixture JSON file at path and returns a
// Provider. Path empty = NewDefaultProvider. The file format is a
// JSON object: {"series": {"<id>": <provider.Series>, ...}}.
func LoadProvider(path string) (*Provider, error) {
	if path == "" {
		return NewDefaultProvider(), nil
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read stub fixture %s: %w", path, err)
	}
	var doc struct {
		Series map[string]provider.Series `json:"series"`
	}
	if err := json.Unmarshal(buf, &doc); err != nil {
		return nil, fmt.Errorf("parse stub fixture %s: %w", path, err)
	}
	return &Provider{seriesByID: doc.Series}, nil
}

// Key returns the provider key string.
func (p *Provider) Key() string { return FixtureProviderKey }

// Search matches the query against fixture preferred / canonical
// titles via case-insensitive substring. Returns every fixture
// series whose title contains the query. Lets free-text CLI
// invocations (`kura show stub-show`) exercise the resolver chain
// in tests instead of forcing every scenario to use metadata refs.
func (p *Provider) Search(_ context.Context, query textnorm.NFCString, _ provider.SearchOptions) ([]provider.SearchResult, error) {
	q := strings.ToLower(query.String())
	if q == "" {
		return nil, nil
	}
	out := make([]provider.SearchResult, 0)
	for _, s := range p.seriesByID {
		if matchesQuery(s.PreferredTitle.String(), q) || matchesQuery(s.CanonicalTitle.String(), q) {
			out = append(out, provider.SearchResult{SeriesSummary: s.SeriesSummary})
		}
	}
	return out, nil
}

func matchesQuery(title, q string) bool {
	if title == "" {
		return false
	}
	return strings.Contains(strings.ToLower(title), q)
}

// GetSeries returns the fixture series for id, or provider.ErrNotFound.
func (p *Provider) GetSeries(_ context.Context, id, _ string) (provider.Series, error) {
	s, ok := p.seriesByID[id]
	if !ok {
		return provider.Series{}, provider.ErrNotFound
	}
	return s, nil
}

// Inspector implements media.Inspector with canned facts.
type Inspector struct {
	resolution string
	videoCodec string
}

// NewDefaultInspector returns an Inspector that reports 1920x1080 H.264
// for every file regardless of content. Lets Stage run without a real
// mediainfo binary on the host.
func NewDefaultInspector() *Inspector {
	return &Inspector{resolution: "1920x1080", videoCodec: "H.264"}
}

// Inspect returns canned media.Info.
func (i *Inspector) Inspect(_ context.Context, _ string) (media.Info, error) {
	return media.Info{
		Resolution: i.resolution,
		VideoCodec: i.videoCodec,
	}, nil
}

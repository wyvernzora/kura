package workflow_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider"
	"github.com/wyvernzora/kura/services/library-manager/internal/textnorm"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

// posterSource is a fake provider.Source whose Search returns two
// candidates (so Resolve treats the result as ambiguous and runs the
// enrichment fetch) and whose GetSeries returns a per-id poster.
type posterSource struct{}

func (posterSource) Key() string { return "tvdb" }

func (posterSource) Search(context.Context, textnorm.NFCString, provider.SearchOptions) ([]provider.SearchResult, error) {
	return []provider.SearchResult{
		{SeriesSummary: provider.SeriesSummary{MetadataRef: mustMeta("tvdb:1"), PreferredTitle: textnorm.NFC("One")}},
		{SeriesSummary: provider.SeriesSummary{MetadataRef: mustMeta("tvdb:2"), PreferredTitle: textnorm.NFC("Two")}},
	}, nil
}

func (posterSource) GetSeries(_ context.Context, id, _ string) (provider.Series, error) {
	return provider.Series{
		SeriesSummary: provider.SeriesSummary{MetadataRef: mustMeta("tvdb:" + id)},
		Poster: provider.Artwork{
			URL:          "https://art/" + id + ".jpg",
			ThumbnailURL: "https://art/" + id + "-thumb.jpg",
		},
	}, nil
}

func (p posterSource) GetSeriesSummary(ctx context.Context, id string) (provider.SeriesSummary, error) {
	series, err := p.GetSeries(ctx, id, "")
	if err != nil {
		return provider.SeriesSummary{}, err
	}
	summary := series.SeriesSummary
	summary.Poster = series.Poster
	return summary, nil
}

// countingSource records which detail call resolve enrichment makes.
// GetSeries walks the provider's full paginated episode spine; using it
// to fetch genres for a 50-candidate list cost hundreds of upstream
// requests and made TVDB search take tens of seconds.
type countingSource struct {
	seriesCalls  atomic.Int32
	summaryCalls atomic.Int32
}

func (*countingSource) Key() string { return "tvdb" }

func (*countingSource) Search(context.Context, textnorm.NFCString, provider.SearchOptions) ([]provider.SearchResult, error) {
	return []provider.SearchResult{
		{SeriesSummary: provider.SeriesSummary{MetadataRef: mustMeta("tvdb:1"), PreferredTitle: textnorm.NFC("One")}},
		{SeriesSummary: provider.SeriesSummary{MetadataRef: mustMeta("tvdb:2"), PreferredTitle: textnorm.NFC("Two")}},
	}, nil
}

func (c *countingSource) GetSeries(context.Context, string, string) (provider.Series, error) {
	c.seriesCalls.Add(1)
	return provider.Series{}, nil
}

func (c *countingSource) GetSeriesSummary(_ context.Context, id string) (provider.SeriesSummary, error) {
	c.summaryCalls.Add(1)
	return provider.SeriesSummary{MetadataRef: mustMeta("tvdb:" + id), Genres: []string{"Anime"}}, nil
}

func TestResolveEnrichesWithoutEpisodeSpine(t *testing.T) {
	source := &countingSource{}
	deps := workflow.Deps{
		Provider: workflow.NewProviderFactory(func() (provider.Source, error) { return source, nil }),
	}
	out, err := workflow.Resolve(context.Background(), deps, workflow.ResolveInput{Terms: []string{"query"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(out.Candidates) != 2 {
		t.Fatalf("len(candidates) = %d, want 2", len(out.Candidates))
	}
	if got := source.seriesCalls.Load(); got != 0 {
		t.Fatalf("GetSeries calls = %d, want 0 (enrichment must not fetch episodes)", got)
	}
	if got := source.summaryCalls.Load(); got != 2 {
		t.Fatalf("GetSeriesSummary calls = %d, want 2", got)
	}
	for _, c := range out.Candidates {
		if len(c.Genres) != 1 || c.Genres[0] != "Anime" {
			t.Fatalf("candidate %s genres = %#v, want enriched", c.Ref, c.Genres)
		}
	}
}

func mustMeta(s string) refs.Metadata {
	m, err := refs.ParseMetadata(s)
	if err != nil {
		panic(err)
	}
	return m
}

// searchPosterSource returns candidates whose search summaries already
// carry a poster, and whose per-candidate detail fetch always fails —
// mimicking TVDB rate-limiting the extended endpoint under concurrency.
type searchPosterSource struct{}

func (searchPosterSource) Key() string { return "tvdb" }

func (searchPosterSource) Search(context.Context, textnorm.NFCString, provider.SearchOptions) ([]provider.SearchResult, error) {
	return []provider.SearchResult{
		{SeriesSummary: provider.SeriesSummary{
			MetadataRef:    mustMeta("tvdb:1"),
			PreferredTitle: textnorm.NFC("One"),
			Poster:         provider.Artwork{URL: "https://search/1.jpg", ThumbnailURL: "https://search/1-t.jpg"},
		}},
		{SeriesSummary: provider.SeriesSummary{
			MetadataRef:    mustMeta("tvdb:2"),
			PreferredTitle: textnorm.NFC("Two"),
			Poster:         provider.Artwork{URL: "https://search/2.jpg", ThumbnailURL: "https://search/2-t.jpg"},
		}},
	}, nil
}

func (searchPosterSource) GetSeries(context.Context, string, string) (provider.Series, error) {
	return provider.Series{}, errors.New("rate limited")
}

func (searchPosterSource) GetSeriesSummary(context.Context, string) (provider.SeriesSummary, error) {
	return provider.SeriesSummary{}, errors.New("rate limited")
}

// TestResolveUsesSearchPosterWhenEnrichmentFails is the regression for
// candidates rendering a placeholder poster: the search poster must
// survive even when the enrichment detail fetch errors out.
func TestResolveUsesSearchPosterWhenEnrichmentFails(t *testing.T) {
	deps := workflow.Deps{
		Provider: workflow.NewProviderFactory(func() (provider.Source, error) {
			return searchPosterSource{}, nil
		}),
	}
	res, err := workflow.Resolve(context.Background(), deps, workflow.ResolveInput{Terms: []string{"anything"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(res.Candidates))
	}
	for _, c := range res.Candidates {
		id := c.Ref.ID()
		if want := "https://search/" + id + ".jpg"; c.PosterURL != want {
			t.Errorf("ref %s posterUrl = %q, want %q", c.Ref, c.PosterURL, want)
		}
		if want := "https://search/" + id + "-t.jpg"; c.PosterThumbnailURL != want {
			t.Errorf("ref %s posterThumbnailUrl = %q, want %q", c.Ref, c.PosterThumbnailURL, want)
		}
	}
}

// TestResolveSurfacesCandidatePosters guards the enrichment fallback:
// when the search summary has no poster, the poster fetched during
// enrichment carries through to the response candidates.
func TestResolveSurfacesCandidatePosters(t *testing.T) {
	deps := workflow.Deps{
		Provider: workflow.NewProviderFactory(func() (provider.Source, error) {
			return posterSource{}, nil
		}),
	}
	res, err := workflow.Resolve(context.Background(), deps, workflow.ResolveInput{Terms: []string{"anything"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(res.Candidates))
	}
	for _, c := range res.Candidates {
		id := c.Ref.ID()
		if want := "https://art/" + id + ".jpg"; c.PosterURL != want {
			t.Errorf("ref %s posterUrl = %q, want %q", c.Ref, c.PosterURL, want)
		}
		if want := "https://art/" + id + "-thumb.jpg"; c.PosterThumbnailURL != want {
			t.Errorf("ref %s posterThumbnailUrl = %q, want %q", c.Ref, c.PosterThumbnailURL, want)
		}
	}
}

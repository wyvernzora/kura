package workflow_test

import (
	"context"
	"errors"
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

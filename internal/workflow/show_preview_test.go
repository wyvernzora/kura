package workflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/textnorm"
	"github.com/wyvernzora/kura/internal/workflow"
)

// previewSource returns a series with a two-episode spine (one aired,
// one far-future) and a poster, so the preview test can assert the
// force-missing collapse (the future episode would otherwise be pending).
type previewSource struct{}

func (previewSource) Key() string { return "tvdb" }

func (previewSource) Search(context.Context, textnorm.NFCString, provider.SearchOptions) ([]provider.SearchResult, error) {
	return nil, nil
}

func (previewSource) GetSeries(_ context.Context, id, _ string) (provider.Series, error) {
	ep1, _ := refs.NewEpisode(1, 1)
	ep2, _ := refs.NewEpisode(1, 2)
	return provider.Series{
		SeriesSummary: provider.SeriesSummary{
			MetadataRef:    mustMeta("tvdb:" + id),
			PreferredTitle: textnorm.NFC("Preview Show"),
		},
		Poster: provider.Artwork{URL: "https://art/p.jpg", ThumbnailURL: "https://art/p-t.jpg"},
		Seasons: []provider.Season{
			{Number: 1, Episodes: []provider.Episode{
				{Ref: ep1, Aired: "2001-01-01"},
				{Ref: ep2, Aired: "2999-01-01"}, // future: pending unless forced missing
			}},
		},
	}, nil
}

// TestShowPreviewBuildsFromProviderAllMissing guards the ?preview path:
// Show must build from live provider metadata (no index / series.json)
// and report every episode as missing, including not-yet-aired ones.
func TestShowPreviewBuildsFromProviderAllMissing(t *testing.T) {
	deps := workflow.Deps{
		LibRoot: t.TempDir(),
		Now:     func() time.Time { return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) },
		Provider: workflow.NewProviderFactory(func() (provider.Source, error) {
			return previewSource{}, nil
		}),
	}

	show, err := workflow.Show(context.Background(), deps, workflow.ShowInput{
		Preview:     true,
		MetadataRef: mustMeta("tvdb:42"),
	})
	if err != nil {
		t.Fatalf("Show preview: %v", err)
	}

	if show.MetadataRef.String() != "tvdb:42" {
		t.Errorf("metadataRef = %q, want tvdb:42", show.MetadataRef)
	}
	if show.Ref.IsZero() {
		t.Error("preview ref is zero; want a derived directory name")
	}
	if show.Artwork == nil || show.Artwork.Poster == nil || show.Artwork.Poster.URL != "https://art/p.jpg" {
		t.Errorf("artwork poster = %+v, want URL https://art/p.jpg", show.Artwork)
	}

	total := 0
	for _, s := range show.Seasons {
		if s.Summary.Missing != s.Summary.EpisodeCount || s.Summary.Pending != 0 || s.Summary.Present != 0 {
			t.Errorf("season %d summary = %+v, want all missing", s.Number, s.Summary)
		}
		for _, e := range s.Episodes {
			total++
			if e.Status != response.StatusMissing {
				t.Errorf("episode %s status = %q, want %q", e.Episode, e.Status, response.StatusMissing)
			}
		}
	}
	if total != 2 {
		t.Fatalf("episodes = %d, want 2", total)
	}
}

package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/wyvernzora/kura/internal/domain/selector"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/resolve"
	"github.com/wyvernzora/kura/internal/response"
)

// ResolveInput parameters for the Resolve workflow.
type ResolveInput struct {
	Terms []string
}

// Resolve runs the configured strategy chain against in.Terms and returns
// the candidate set. The workflow does not auto-pick, prompt, or error on
// ambiguity; callers inspect the candidate-list cardinality and decide.
//
// Provider-needing: invokes deps.Provider() lazily.
func Resolve(ctx context.Context, deps Deps, in ResolveInput) (response.Resolution, error) {
	progress.Start(ctx, "resolve", fmt.Sprintf("Resolving %s", strings.Join(in.Terms, " ")), 0)
	source, err := deps.Provider()
	if err != nil {
		progress.Failure(ctx, "resolve", "Failed to resolve series", 0, 0)
		return response.Resolution{}, err
	}
	resolver := resolve.New(
		resolve.NewMetadataIDStrategy(source),
		resolve.NewTextSearchStrategy(source),
	)
	res, err := resolver.Resolve(ctx, selector.ParseSelector(in.Terms))
	if err != nil {
		progress.Failure(ctx, "resolve", "Failed to resolve series", 0, 0)
		return response.Resolution{}, err
	}
	// Search results carry no genres or artwork on TVDB (search endpoint
	// omits them). Enrich ambiguous results with a parallel detail fetch
	// so the agent can tell an anime adaptation from a live-action one
	// and UI surfaces can show candidate posters. Single-match results
	// skip the round-trip — no disambiguation needed.
	//
	// ponytail: singleton resolve shows placeholder art; drop the >1 gate
	// if posters are wanted on unique matches too.
	var enriched map[string]enrichment
	if len(res.Results) > 1 {
		enriched = enrichCandidates(ctx, source, res.Results)
	}
	progress.Success(ctx, "resolve", "Resolved series", len(res.Results))

	out := response.Resolution{
		Candidates: make([]response.Candidate, 0, len(res.Results)),
	}
	for _, r := range res.Results {
		genres := r.Summary.Genres
		// Poster comes from the search response (reliable, one request);
		// the enrichment fetch only supplies genres and a poster
		// fallback for the rare record with no search image.
		poster := r.Summary.Poster
		if got, ok := enriched[r.Summary.MetadataRef.String()]; ok {
			genres = got.genres
			if poster.URL == "" {
				poster = provider.Artwork{URL: got.posterURL, ThumbnailURL: got.posterThumb}
			}
		}
		out.Candidates = append(out.Candidates, candidateFrom(r, genres, poster))
	}
	return out, nil
}

// enrichment holds the per-candidate facts pulled from a detail fetch
// that the lightweight search result lacks.
type enrichment struct {
	genres      []string
	posterURL   string
	posterThumb string
}

// candidateFrom builds a response.Candidate from a resolve.Result plus
// the resolved genres and poster.
func candidateFrom(r resolve.Result, genres []string, poster provider.Artwork) response.Candidate {
	evidence := make([]response.Evidence, 0, len(r.Evidence))
	for _, ev := range r.Evidence {
		evidence = append(evidence, response.Evidence{
			Term:        ev.Term,
			Rank:        ev.Rank,
			MatchSource: ev.MatchSource,
			Annotations: ev.Annotations,
		})
	}
	return response.Candidate{
		Ref:                r.Summary.MetadataRef,
		PreferredTitle:     r.Summary.PreferredTitle.String(),
		CanonicalTitle:     r.Summary.CanonicalTitle.String(),
		Year:               r.Summary.Year,
		FirstAired:         r.Summary.FirstAired,
		OriginalLanguage:   r.Summary.OriginalLanguage,
		OriginalCountry:    r.Summary.OriginalCountry,
		Genres:             append([]string(nil), genres...),
		PosterURL:          poster.URL,
		PosterThumbnailURL: poster.ThumbnailURL,
		Evidence:           evidence,
	}
}

// enrichCandidates pulls per-candidate genres and poster art in parallel
// via source.GetSeries. Failures per-candidate fall back to no
// enrichment (best-effort, never breaks the resolve).
func enrichCandidates(ctx context.Context, source provider.Source, results []resolve.Result) map[string]enrichment {
	out := make(map[string]enrichment, len(results))
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for _, r := range results {
		ref := r.Summary.MetadataRef
		g.Go(func() error {
			series, err := source.GetSeries(gctx, ref.ID(), "")
			if err != nil {
				return nil
			}
			mu.Lock()
			out[ref.String()] = enrichment{
				genres:      append([]string(nil), series.SeriesSummary.Genres...),
				posterURL:   series.Poster.URL,
				posterThumb: series.Poster.ThumbnailURL,
			}
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return out
}

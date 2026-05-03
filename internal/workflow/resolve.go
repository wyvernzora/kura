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
	// Search results carry no genres on TVDB (search endpoint omits
	// them). Enrich ambiguous results with a parallel detail fetch so
	// the agent can tell an anime adaptation from a live-action one.
	// Single-match results skip the round-trip — no disambiguation
	// needed.
	var genreByRef map[string][]string
	if len(res.Results) > 1 {
		genreByRef = fetchGenres(ctx, source, res.Results)
	}
	progress.Success(ctx, "resolve", "Resolved series", len(res.Results))

	out := response.Resolution{
		Candidates: make([]response.Candidate, 0, len(res.Results)),
	}
	for _, r := range res.Results {
		genres := r.Summary.Genres
		if g, ok := genreByRef[r.Summary.MetadataRef.String()]; ok {
			genres = g
		}
		out.Candidates = append(out.Candidates, candidateFrom(r, genres))
	}
	return out, nil
}

// candidateFrom builds a response.Candidate from a resolve.Result,
// substituting an externally-fetched genres slice when available.
func candidateFrom(r resolve.Result, genres []string) response.Candidate {
	evidence := make([]response.Evidence, 0, len(r.Evidence))
	for _, e := range r.Evidence {
		evidence = append(evidence, response.Evidence{
			Term:        e.Term,
			Rank:        e.Rank,
			MatchSource: e.MatchSource,
			Annotations: e.Annotations,
		})
	}
	return response.Candidate{
		Ref:              r.Summary.MetadataRef,
		PreferredTitle:   r.Summary.PreferredTitle.String(),
		CanonicalTitle:   r.Summary.CanonicalTitle.String(),
		Year:             r.Summary.Year,
		FirstAired:       r.Summary.FirstAired,
		OriginalLanguage: r.Summary.OriginalLanguage,
		OriginalCountry:  r.Summary.OriginalCountry,
		Genres:           append([]string(nil), genres...),
		Evidence:         evidence,
	}
}

// fetchGenres pulls per-candidate genres in parallel via
// source.GetSeries. Failures per-candidate fall back to no genres
// (best-effort enrichment, never breaks the resolve).
func fetchGenres(ctx context.Context, source provider.Source, results []resolve.Result) map[string][]string {
	out := make(map[string][]string, len(results))
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
			out[ref.String()] = append([]string(nil), series.SeriesSummary.Genres...)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return out
}

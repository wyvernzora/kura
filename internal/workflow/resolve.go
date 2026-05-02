package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/wyvernzora/kura/internal/domain/selector"
	"github.com/wyvernzora/kura/internal/progress"
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
	progress.Success(ctx, "resolve", "Resolved series", len(res.Results))

	out := response.Resolution{
		Candidates: make([]response.Candidate, 0, len(res.Results)),
	}
	for _, r := range res.Results {
		out.Candidates = append(out.Candidates, response.Candidate{
			Ref:              r.Summary.MetadataRef,
			PreferredTitle:   r.Summary.PreferredTitle.String(),
			CanonicalTitle:   r.Summary.CanonicalTitle.String(),
			Year:             r.Summary.Year,
			FirstAired:       r.Summary.FirstAired,
			OriginalLanguage: r.Summary.OriginalLanguage,
			OriginalCountry:  r.Summary.OriginalCountry,
		})
	}
	return out, nil
}

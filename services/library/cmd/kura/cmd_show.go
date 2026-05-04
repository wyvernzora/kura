package main

import (
	"errors"
	"fmt"

	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/cli/client"
	"github.com/wyvernzora/kura/internal/cli/prompt"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

type showCmd struct {
	JSON       bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Episodes   string   `name:"episodes" help:"Episode selector: S<N>, S<N>E<E>, or S<N>E<A>-<B>. Specials = S0."`
	Status     []string `name:"status" help:"Repeatable. Restrict to episodes whose status is in this set (pending, missing, present, unavailable, staged, staged_replacement)."`
	Source     []string `name:"source" help:"Repeatable. Restrict to active media whose source is in this set (e.g. BluRay, WebRip)."`
	Resolution []string `name:"resolution" help:"Repeatable. Restrict to active media whose resolution is in this set (e.g. 1080p, 720p)."`
	Terms      []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *showCmd) Run(rt *runContext) error {
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, cmd.Terms)
	if err != nil {
		return err
	}
	result, err := c.ShowSeries(rt.Context, ref, client.ShowOptions{
		Episodes:   cmd.Episodes,
		Status:     cmd.Status,
		Source:     cmd.Source,
		Resolution: cmd.Resolution,
	})
	if err != nil {
		return err
	}
	return render.Show(rt.Stdout, result, cmd.JSON)
}

// resolveTermsToRef turns CLI terms into a MetadataRef string for
// REST endpoints. Per Product.md "Selectors, not paths" every
// REST resource endpoint identifies series by metadata ref;
// SeriesRef is purely an internal storage concern. Mirrors the
// pre-REST `clipkg.WithResolve` UX:
//
//   - Single term parseable as a metadata ref (provider:id) →
//     pass through verbatim. No /resolve round-trip.
//   - Other terms → POST /resolve. 0 candidates: error.
//     1 candidate: use it. Multi: prompt the user (errors loudly
//     when stdin isn't a TTY).
func resolveTermsToRef(rt *runContext, c *client.Client, io stdio.Stdio, terms []string) (string, error) {
	if len(terms) == 0 {
		return "", fmt.Errorf("at least one term required")
	}
	if len(terms) == 1 {
		if _, err := refs.ParseMetadata(terms[0]); err == nil {
			return terms[0], nil
		}
	}
	resolution, err := c.Resolve(rt.Context, terms)
	if err != nil {
		return "", err
	}
	switch len(resolution.Candidates) {
	case 0:
		return "", fmt.Errorf("%w: %v", clipkg.ErrNoMetadataMatch, terms)
	case 1:
		return resolution.Candidates[0].Ref.String(), nil
	}
	picked, err := prompt.SelectCandidate(io, terms, resolution.Candidates)
	if err != nil {
		if errors.Is(err, prompt.ErrNonInteractive) {
			return "", fmt.Errorf("%w: selector %v matched %d candidates; pass a metadata ref directly to disambiguate",
				clipkg.ErrAmbiguousSelector, terms, len(resolution.Candidates))
		}
		return "", err
	}
	return picked.Ref.String(), nil
}

package main

import (
	"github.com/wyvernzora/kura/services/library/internal/cli/client"
	"github.com/wyvernzora/kura/services/library/internal/cli/render"
	"github.com/wyvernzora/kura/services/library/internal/cli/stdio"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/domain/selector"
	"github.com/wyvernzora/kura/services/library/internal/provider/tvdb"
)

type importCmd struct {
	Dirname  string   `arg:"" required:"" help:"Existing directory below KURA_LIBRARY_ROOT."`
	JSON     bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Force    bool     `name:"force" help:"Replace existing .kura/series.json while preserving other .kura contents."`
	Ordering string   `name:"ordering" help:"Pin the per-series episode ordering used for the initial spine fetch. One of: default, official, dvd, absolute, alternate, regional. Omit to use the provider's default."`
	Terms    []string `arg:"" optional:"" help:"Additional resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *importCmd) Run(rt *runContext) error {
	ordering, err := tvdb.ParseOrdering(cmd.Ordering)
	if err != nil {
		return err
	}
	terms := cmd.resolveTerms()
	if _, err := refs.ParseSeries(cmd.Dirname); err != nil {
		return err
	}
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	metaRef, err := resolveTermsToRef(rt, c, io, terms)
	if err != nil {
		return err
	}
	result, err := c.ImportSeries(rt.Context, client.ImportRequest{
		Ref:      metaRef,
		Dirname:  cmd.Dirname,
		Force:    cmd.Force,
		Ordering: ordering,
	})
	if err != nil {
		return err
	}
	return render.Add(rt.Stdout, result, "Imported", cmd.JSON)
}

func (cmd *importCmd) resolveTerms() []string {
	tvdbTerms := 0
	nonEmptyTerms := 0
	var tvdbTerm string

	for _, raw := range cmd.Terms {
		term := selector.ParseTerm(raw)
		if term == "" {
			continue
		}
		nonEmptyTerms++
		ref, err := refs.ParseMetadata(term.String())
		if err == nil && ref.Provider() == "tvdb" {
			tvdbTerms++
			tvdbTerm = raw
		}
	}

	if tvdbTerms == 1 && nonEmptyTerms == 1 {
		return []string{tvdbTerm}
	}
	if nonEmptyTerms > 0 && tvdbTerms > 0 {
		return cmd.Terms
	}

	terms := make([]string, 0, len(cmd.Terms)+1)
	terms = append(terms, cmd.Dirname)
	terms = append(terms, cmd.Terms...)
	return terms
}

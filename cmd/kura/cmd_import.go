package main

import (
	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/selector"
	"github.com/wyvernzora/kura/internal/workflow"
)

type importCmd struct {
	Dirname string   `arg:"" required:"" help:"Existing directory below KURA_LIBRARY_ROOT."`
	JSON    bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Force   bool     `name:"force" help:"Replace existing .kura/series.json while preserving other .kura contents."`
	Terms   []string `arg:"" optional:"" help:"Additional resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *importCmd) Run(rt *runContext) error {
	terms := cmd.resolveTerms()
	ref, err := refs.ParseSeries(cmd.Dirname)
	if err != nil {
		return err
	}
	deps, err := buildDeps(rt)
	if err != nil {
		return err
	}
	io := stdio.From(rt.Context)
	return clipkg.WithResolve(rt.Context, io, deps, terms, func(metadataRef refs.Metadata) error {
		result, err := workflow.Import(rt.Context, deps, workflow.ImportInput{
			Ref:      ref,
			Metadata: metadataRef,
			Force:    cmd.Force,
		})
		if err != nil {
			return err
		}
		return render.Add(rt.Stdout, result, "Imported", cmd.JSON)
	})
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

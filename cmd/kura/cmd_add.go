package main

import (
	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/workflow"
)

type addCmd struct {
	Dirname string   `name:"dirname" help:"Directory name override; defaults to preferred title."`
	JSON    bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Terms   []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *addCmd) Run(rt *runContext) error {
	deps, err := buildDeps(rt)
	if err != nil {
		return err
	}
	io := stdio.From(rt.Context)
	return clipkg.WithResolve(rt.Context, io, deps, cmd.Terms, func(metadataRef refs.Metadata) error {
		var ref refs.Series
		if cmd.Dirname != "" {
			ref, err = refs.ParseSeries(cmd.Dirname)
			if err != nil {
				return err
			}
		}
		result, err := workflow.Add(rt.Context, deps, workflow.AddInput{Metadata: metadataRef, Ref: ref})
		if err != nil {
			return err
		}
		return render.Add(rt.Stdout, result, "Added", cmd.JSON)
	})
}

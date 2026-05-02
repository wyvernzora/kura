package main

import (
	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/ui/stdio"
	"github.com/wyvernzora/kura/internal/workflow"
)

type showCmd struct {
	JSON  bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Terms []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *showCmd) Run(rt *runContext) error {
	deps, err := buildDeps(rt)
	if err != nil {
		return err
	}
	io := stdio.From(rt.Context)
	return clipkg.WithResolve(rt.Context, io, deps, cmd.Terms, func(metadataRef refs.Metadata) error {
		seriesRef, ok, err := deps.Index.Get(metadataRef)
		if err != nil {
			return err
		}
		if !ok {
			return &workflow.NotFoundError{}
		}
		result, err := workflow.Show(rt.Context, deps, workflow.ShowInput{Ref: seriesRef})
		if err != nil {
			return err
		}
		return render.Show(rt.Stdout, result, cmd.JSON)
	})
}

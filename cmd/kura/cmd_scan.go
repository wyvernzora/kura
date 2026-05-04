package main

import (
	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/workflow"
)

type scanCmd struct {
	JSON    bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Refresh bool     `name:"refresh" help:"Force re-run of mediainfo and source detection on every active record, even when size and mtime are unchanged. A freshly detected Unknown source will not overwrite an existing non-Unknown one."`
	Terms   []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *scanCmd) Run(rt *runContext) error {
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
			return &workflow.MetadataRefNotIndexedError{Ref: metadataRef}
		}
		j := workflow.Scan(rt.Context, deps, workflow.ScanInput{Ref: seriesRef, Refresh: cmd.Refresh})
		result, err := j.Wait(rt.Context)
		if err != nil {
			return err
		}
		return render.Scan(rt.Stdout, result, cmd.JSON)
	})
}

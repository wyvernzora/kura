package main

import (
	"fmt"

	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/workflow"
)

type pathCmd struct {
	SeriesFile bool     `name:"seriesfile" help:"Print the path to series.json instead of the series root."`
	Terms      []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *pathCmd) Run(rt *runContext) error {
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
		out := paths.SeriesDir(deps.LibRoot, seriesRef)
		if cmd.SeriesFile {
			out = paths.SeriesMetadata(deps.LibRoot, seriesRef)
		}
		_, err = fmt.Fprintln(rt.Stdout, out)
		return err
	})
}

package main

import (
	"github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/refs"
)

type addCmd struct {
	Dirname string   `name:"dirname" help:"Directory name override; defaults to preferred title."`
	JSON    bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Terms   []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *addCmd) Run(rt *runContext) error {
	lib, err := libraryFromFlags(rt, rt.flags)
	if err != nil {
		return err
	}
	metadataRef, err := resolveMetadataRef(rt, lib, cmd.Terms)
	if err != nil {
		return err
	}

	var ref refs.Series
	if cmd.Dirname != "" {
		ref, err = refs.ParseSeries(cmd.Dirname)
		if err != nil {
			return err
		}
	}
	series, err := lib.Add(rt.Context, library.AddInput{Metadata: metadataRef, Ref: ref})
	if err != nil {
		return err
	}
	return writeSeriesSummary(rt, series, "Added", cmd.JSON)
}

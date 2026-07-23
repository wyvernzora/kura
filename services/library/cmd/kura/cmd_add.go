package main

import (
	"github.com/wyvernzora/kura/services/library/internal/cli/client"
	"github.com/wyvernzora/kura/services/library/internal/cli/render"
	"github.com/wyvernzora/kura/services/library/internal/cli/stdio"
	"github.com/wyvernzora/kura/services/library/internal/provider/tvdb"
)

type addCmd struct {
	Dirname  string   `name:"dirname" help:"Directory name override; defaults to preferred title."`
	JSON     bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Ordering string   `name:"ordering" help:"Pin the per-series episode ordering used for the initial spine fetch. One of: default, official, dvd, absolute, alternate, regional. Omit to use the provider's default."`
	Terms    []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *addCmd) Run(rt *runContext) error {
	ordering, err := tvdb.ParseOrdering(cmd.Ordering)
	if err != nil {
		return err
	}
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, cmd.Terms)
	if err != nil {
		return err
	}
	result, err := c.AddSeries(rt.Context, client.AddRequest{
		Ref:      ref,
		Dirname:  cmd.Dirname,
		Ordering: ordering,
	})
	if err != nil {
		return err
	}
	return render.Add(rt.Stdout, result, "Added", cmd.JSON)
}

package main

import (
	"errors"

	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/cli/stdio"
)

type removeCmd struct {
	JSON    bool     `name:"json" help:"Force JSON output even on TTY."`
	Purge   bool     `name:"purge" help:"Wholesale delete the entire series directory. Requires --confirm."`
	Confirm bool     `name:"confirm" help:"Required with --purge."`
	Terms   []string `arg:"" required:"" help:"Resolver terms identifying the series to remove."`
}

func (cmd *removeCmd) Run(rt *runContext) error {
	if cmd.Purge && !cmd.Confirm {
		return errors.New("remove --purge requires --confirm")
	}
	c := clientFromRT(rt)
	if cmd.Purge {
		c = c.AsOperator()
	}
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, cmd.Terms)
	if err != nil {
		return err
	}
	result, err := c.RemoveSeries(rt.Context, ref, cmd.Purge)
	if err != nil {
		return err
	}
	return render.Remove(rt.Stdout, result, ref, cmd.Purge, cmd.JSON)
}

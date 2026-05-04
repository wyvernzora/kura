package main

import (
	"errors"

	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/workflow"
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
		result, err := workflow.Remove(rt.Context, deps, workflow.RemoveInput{Ref: seriesRef, Purge: cmd.Purge})
		if err != nil {
			return err
		}
		return render.Remove(rt.Stdout, result, seriesRef.String(), cmd.Purge, cmd.JSON)
	})
}

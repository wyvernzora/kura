package main

import (
	"fmt"

	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/workflow"
)

type resolveCmd struct {
	JSON  bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Limit int      `help:"Maximum number of candidates to print. Zero prints all results."`
	Terms []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *resolveCmd) Run(rt *runContext) error {
	if cmd.Limit < 0 {
		return fmt.Errorf("--limit must be greater than or equal to zero")
	}
	deps, err := buildDeps(rt)
	if err != nil {
		return err
	}
	result, err := workflow.Resolve(rt.Context, deps, workflow.ResolveInput{Terms: cmd.Terms})
	if err != nil {
		return err
	}
	if cmd.Limit > 0 && len(result.Candidates) > cmd.Limit {
		result.Candidates = result.Candidates[:cmd.Limit]
	}
	return render.Resolve(rt.Stdout, result, cmd.JSON)
}

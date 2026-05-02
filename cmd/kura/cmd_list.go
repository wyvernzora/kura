package main

import (
	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/workflow"
)

type listCmd struct {
	JSON     bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Statuses []string `name:"status" help:"Only show entries with this status. Repeat for multiple statuses."`
}

func (cmd *listCmd) Run(rt *runContext) error {
	statuses := make([]response.ListStatus, 0, len(cmd.Statuses))
	for _, raw := range cmd.Statuses {
		status, err := clipkg.ParseListStatus(raw)
		if err != nil {
			return err
		}
		statuses = append(statuses, status)
	}
	deps, err := buildDeps(rt)
	if err != nil {
		return err
	}
	result, err := workflow.List(rt.Context, deps, workflow.ListInput{Statuses: statuses})
	if err != nil {
		return err
	}
	return render.List(rt.Stdout, result, cmd.JSON)
}

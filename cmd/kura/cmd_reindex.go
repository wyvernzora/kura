package main

import (
	"github.com/wyvernzora/kura/internal/workflow"
)

type reindexCmd struct{}

func (cmd *reindexCmd) Run(rt *runContext) error {
	deps, err := buildDeps(rt)
	if err != nil {
		return err
	}
	return workflow.Reindex(rt.Context, deps)
}

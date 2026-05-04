package main

type reindexCmd struct{}

func (cmd *reindexCmd) Run(rt *runContext) error {
	c := operatorClientFromRT(rt)
	return c.Reindex(rt.Context)
}

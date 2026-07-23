package main

import (
	"io"

	"github.com/wyvernzora/kura/services/library/internal/cli/stdio"
	"github.com/wyvernzora/kura/services/library/internal/response"
)

type tagCmd struct {
	Update tagUpdateCmd `cmd:"" help:"Atomically add and remove series tags."`
}

type tagUpdateCmd struct {
	JSON bool     `name:"json" help:"Print the resulting tag set as JSON."`
	Ref  string   `arg:"" required:"" help:"Metadata ref of the series, e.g. tvdb:370070."`
	Tags []string `name:"tag" required:"" help:"Tag change. Plain tags add; !tag removes. Repeat for multiple changes."`
}

func (cmd *tagUpdateCmd) Run(rt *runContext) error {
	out, err := clientFromRT(rt).UpdateTags(rt.Context, cmd.Ref, cmd.Tags)
	if err != nil {
		return err
	}
	return printTags(stdio.From(rt.Context).Out, out, cmd.JSON)
}

func printTags(out io.Writer, result response.SeriesTags, asJSON bool) error {
	return printStringList(out, result, result.Tags, "(no tags)", asJSON)
}

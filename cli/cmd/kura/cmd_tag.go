package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/wyvernzora/kura/cli/internal/cli/stdio"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
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

// printTags renders the tag set as a single-column human read or as
// JSON. The human form stays minimal so it pipes into shell loops.
func printTags(out io.Writer, result api.SeriesTags, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	if len(result.Tags) == 0 {
		_, err := fmt.Fprintln(out, "(no tags)")
		return err
	}
	for _, tag := range result.Tags {
		if _, err := fmt.Fprintln(out, tag); err != nil {
			return err
		}
	}
	return nil
}

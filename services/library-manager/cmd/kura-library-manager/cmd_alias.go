package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/wyvernzora/kura/services/library-manager/internal/cli/stdio"
	"github.com/wyvernzora/kura/services/library-manager/internal/response"
)

// aliasCmd is the `kura alias` subcommand group. Adds, removes, or
// lists user-coined search shorthands for a tracked series. The
// shorthands fold into the per-row `searchKey` blob the web UI uses
// for local fuzzy search; TVDB-derived aliases come in via scan and
// are intentionally not editable here.
type aliasCmd struct {
	Add  aliasAddCmd  `cmd:"" help:"Append one or more aliases to a series."`
	Rm   aliasRmCmd   `cmd:"" help:"Drop one or more aliases from a series."`
	List aliasListCmd `cmd:"" help:"List the persisted user aliases for a series."`
}

type aliasAddCmd struct {
	JSON  bool     `name:"json" help:"Print the resulting alias list as JSON."`
	Ref   string   `arg:"" required:"" help:"Metadata ref of the series, e.g. tvdb:370070."`
	Alias []string `arg:"" required:"" help:"One or more shorthand aliases to add."`
}

type aliasRmCmd struct {
	JSON  bool     `name:"json" help:"Print the resulting alias list as JSON."`
	Ref   string   `arg:"" required:"" help:"Metadata ref of the series."`
	Alias []string `arg:"" required:"" help:"One or more aliases to drop."`
}

type aliasListCmd struct {
	JSON bool   `name:"json" help:"Print the alias list as JSON."`
	Ref  string `arg:"" required:"" help:"Metadata ref of the series."`
}

func (cmd *aliasAddCmd) Run(rt *runContext) error {
	if err := requireAliases(cmd.Alias); err != nil {
		return err
	}
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	out, err := c.AddUserAliases(rt.Context, cmd.Ref, cmd.Alias)
	if err != nil {
		return err
	}
	return printAliases(io.Out, out, cmd.JSON)
}

func (cmd *aliasRmCmd) Run(rt *runContext) error {
	if err := requireAliases(cmd.Alias); err != nil {
		return err
	}
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	out, err := c.RemoveUserAliases(rt.Context, cmd.Ref, cmd.Alias)
	if err != nil {
		return err
	}
	return printAliases(io.Out, out, cmd.JSON)
}

func (cmd *aliasListCmd) Run(rt *runContext) error {
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	out, err := c.ListUserAliases(rt.Context, cmd.Ref)
	if err != nil {
		return err
	}
	return printAliases(io.Out, out, cmd.JSON)
}

func requireAliases(aliases []string) error {
	for _, alias := range aliases {
		if strings.TrimSpace(alias) != "" {
			return nil
		}
	}
	return errors.New("at least one non-empty alias is required")
}

// printAliases renders the alias list as a single-column human read
// or as JSON. The human form intentionally stays minimal so it pipes
// cleanly into shell loops.
func printAliases(out io.Writer, list response.UserAliasList, asJSON bool) error {
	return printStringList(out, list, list.Aliases, "(no user aliases)", asJSON)
}

func printStringList(out io.Writer, jsonPayload any, items []string, emptyMsg string, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(jsonPayload)
	}
	if len(items) == 0 {
		_, err := fmt.Fprintln(out, emptyMsg)
		return err
	}
	for _, item := range items {
		if _, err := fmt.Fprintln(out, item); err != nil {
			return err
		}
	}
	return nil
}

package main

import (
	"errors"
	"fmt"

	"github.com/oklog/ulid/v2"

	clipkg "github.com/wyvernzora/kura/services/library/internal/cli"
	"github.com/wyvernzora/kura/services/library/internal/cli/render"
	"github.com/wyvernzora/kura/services/library/internal/cli/stdio"
)

type trashCmd struct {
	List    trashListCmd    `cmd:"" help:"List trashed entries."`
	Empty   trashEmptyCmd   `cmd:"" help:"Permanently delete trashed entries."`
	Restore trashRestoreCmd `cmd:"" help:"Restore a trashed entry's files to their recorded paths."`
}

type trashListCmd struct {
	JSON      bool     `name:"json" help:"Force JSON output even on TTY."`
	All       bool     `name:"all" help:"List trash across the entire library."`
	OlderThan string   `name:"older-than" help:"Only list entries older than DURATION (e.g. 30d, 48h, 2w)."`
	Terms     []string `arg:"" optional:"" help:"Resolver terms. Required unless --all."`
}

type trashEmptyCmd struct {
	JSON      bool     `name:"json" help:"Force JSON output even on TTY."`
	All       bool     `name:"all" help:"Empty trash across the entire library. Requires --confirm."`
	Confirm   bool     `name:"confirm" help:"Required with --all."`
	OlderThan string   `name:"older-than" help:"Only empty entries older than DURATION (e.g. 30d, 48h, 2w)."`
	Terms     []string `arg:"" optional:"" help:"Resolver terms. Required unless --all."`
}

type trashRestoreCmd struct {
	JSON bool     `name:"json" help:"Force JSON output even on TTY."`
	Args []string `arg:"" required:"" help:"Resolver terms followed by the trash ULID."`
}

func (cmd *trashListCmd) Run(rt *runContext) error {
	if err := validateTrashSelector(cmd.Terms, cmd.All); err != nil {
		return err
	}
	if _, err := clipkg.ParseDuration(cmd.OlderThan); err != nil {
		return err
	}
	c := clientFromRT(rt)
	if cmd.All {
		result, err := c.TrashListAll(rt.Context, cmd.OlderThan)
		if err != nil {
			return err
		}
		return render.TrashList(rt.Stdout, result, cmd.JSON)
	}
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, cmd.Terms)
	if err != nil {
		return err
	}
	result, err := c.TrashListSeries(rt.Context, ref, cmd.OlderThan)
	if err != nil {
		return err
	}
	return render.TrashList(rt.Stdout, result, cmd.JSON)
}

func (cmd *trashEmptyCmd) Run(rt *runContext) error {
	if err := validateTrashSelector(cmd.Terms, cmd.All); err != nil {
		return err
	}
	if cmd.All && !cmd.Confirm {
		return errors.New("trash empty across the entire library requires --confirm")
	}
	if _, err := clipkg.ParseDuration(cmd.OlderThan); err != nil {
		return err
	}
	c := operatorClientFromRT(rt)
	if cmd.All {
		result, err := c.TrashEmptyAll(rt.Context, cmd.OlderThan)
		if err != nil {
			return err
		}
		return render.TrashEmpty(rt.Stdout, result, cmd.JSON)
	}
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, cmd.Terms)
	if err != nil {
		return err
	}
	result, err := c.TrashEmptySeries(rt.Context, ref, cmd.OlderThan)
	if err != nil {
		return err
	}
	return render.TrashEmpty(rt.Stdout, result, cmd.JSON)
}

func (cmd *trashRestoreCmd) Run(rt *runContext) error {
	terms, idStr, err := splitTrashRestoreArgs(cmd.Args)
	if err != nil {
		return err
	}
	if _, err := ulid.ParseStrict(idStr); err != nil {
		return fmt.Errorf("invalid trash ULID %q: %w", idStr, err)
	}
	c := operatorClientFromRT(rt)
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, terms)
	if err != nil {
		return err
	}
	result, err := c.TrashRestore(rt.Context, ref, idStr)
	if err != nil {
		return err
	}
	return render.TrashRestore(rt.Stdout, result, ref, cmd.JSON)
}

func validateTrashSelector(terms []string, all bool) error {
	if all && len(terms) > 0 {
		return errors.New("trash invocation accepts either selector terms or --all, not both")
	}
	if !all && len(terms) == 0 {
		return errors.New("trash invocation requires selector terms or --all")
	}
	return nil
}

func splitTrashRestoreArgs(args []string) (terms []string, trashID string, err error) {
	if len(args) < 2 {
		return nil, "", errors.New("trash restore requires at least one selector term and a trash ULID")
	}
	return args[:len(args)-1], args[len(args)-1], nil
}

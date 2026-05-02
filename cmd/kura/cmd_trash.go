package main

import (
	"errors"
	"fmt"

	"github.com/oklog/ulid/v2"
	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/workflow"
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
	olderThan, err := clipkg.ParseDuration(cmd.OlderThan)
	if err != nil {
		return err
	}
	deps, err := buildDeps(rt)
	if err != nil {
		return err
	}
	io := stdio.From(rt.Context)
	if cmd.All {
		result, err := workflow.TrashList(rt.Context, deps, workflow.TrashListInput{All: true, OlderThan: olderThan})
		if err != nil {
			return err
		}
		return render.TrashList(rt.Stdout, result, cmd.JSON)
	}
	return clipkg.WithResolve(rt.Context, io, deps, cmd.Terms, func(metadataRef refs.Metadata) error {
		seriesRef, ok, err := deps.Index.Get(metadataRef)
		if err != nil {
			return err
		}
		if !ok {
			return &workflow.MetadataRefNotIndexedError{Ref: metadataRef}
		}
		result, err := workflow.TrashList(rt.Context, deps, workflow.TrashListInput{Ref: seriesRef, OlderThan: olderThan})
		if err != nil {
			return err
		}
		return render.TrashList(rt.Stdout, result, cmd.JSON)
	})
}

func (cmd *trashEmptyCmd) Run(rt *runContext) error {
	if err := validateTrashSelector(cmd.Terms, cmd.All); err != nil {
		return err
	}
	if cmd.All && !cmd.Confirm {
		return errors.New("trash empty across the entire library requires --confirm")
	}
	olderThan, err := clipkg.ParseDuration(cmd.OlderThan)
	if err != nil {
		return err
	}
	deps, err := buildDeps(rt)
	if err != nil {
		return err
	}
	io := stdio.From(rt.Context)
	if cmd.All {
		result, err := workflow.TrashEmpty(rt.Context, deps, workflow.TrashEmptyInput{All: true, OlderThan: olderThan})
		if err != nil {
			return err
		}
		return render.TrashEmpty(rt.Stdout, result, cmd.JSON)
	}
	return clipkg.WithResolve(rt.Context, io, deps, cmd.Terms, func(metadataRef refs.Metadata) error {
		seriesRef, ok, err := deps.Index.Get(metadataRef)
		if err != nil {
			return err
		}
		if !ok {
			return &workflow.MetadataRefNotIndexedError{Ref: metadataRef}
		}
		result, err := workflow.TrashEmpty(rt.Context, deps, workflow.TrashEmptyInput{Ref: seriesRef, OlderThan: olderThan})
		if err != nil {
			return err
		}
		return render.TrashEmpty(rt.Stdout, result, cmd.JSON)
	})
}

func (cmd *trashRestoreCmd) Run(rt *runContext) error {
	terms, idStr, err := splitTrashRestoreArgs(cmd.Args)
	if err != nil {
		return err
	}
	id, err := ulid.ParseStrict(idStr)
	if err != nil {
		return fmt.Errorf("invalid trash ULID %q: %w", idStr, err)
	}
	deps, err := buildDeps(rt)
	if err != nil {
		return err
	}
	io := stdio.From(rt.Context)
	return clipkg.WithResolve(rt.Context, io, deps, terms, func(metadataRef refs.Metadata) error {
		seriesRef, ok, err := deps.Index.Get(metadataRef)
		if err != nil {
			return err
		}
		if !ok {
			return &workflow.MetadataRefNotIndexedError{Ref: metadataRef}
		}
		result, err := workflow.TrashRestore(rt.Context, deps, workflow.TrashRestoreInput{Ref: seriesRef, ID: id})
		if err != nil {
			return err
		}
		return render.TrashRestore(rt.Stdout, result, cmd.JSON)
	})
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

func splitTrashRestoreArgs(args []string) ([]string, string, error) {
	if len(args) < 2 {
		return nil, "", errors.New("trash restore requires at least one selector term and a trash ULID")
	}
	return args[:len(args)-1], args[len(args)-1], nil
}

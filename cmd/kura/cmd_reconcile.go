package main

import (
	"errors"

	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/workflow"
)

type reconcileCmd struct {
	Plan  reconcilePlanCmd  `cmd:"" help:"Create a reconcile plan for a tracked series."`
	Apply reconcileApplyCmd `cmd:"" help:"Apply a saved reconcile plan for a tracked series."`
}

type reconcilePlanCmd struct {
	JSON  bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Terms []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

type reconcileApplyCmd struct {
	JSON bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Args []string `arg:"" required:"" help:"Resolver terms followed by the reconcile plan token."`
}

func (cmd *reconcilePlanCmd) Run(rt *runContext) error {
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
		result, err := workflow.PlanReconcile(rt.Context, deps, workflow.PlanReconcileInput{Ref: seriesRef})
		if err != nil {
			return err
		}
		return render.PlanReconcile(rt.Stdout, result, cmd.JSON)
	})
}

func (cmd *reconcileApplyCmd) Run(rt *runContext) error {
	terms, token, err := splitReconcileApplyArgs(cmd.Args)
	if err != nil {
		return err
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
		result, err := workflow.ApplyReconcile(rt.Context, deps, workflow.ApplyReconcileInput{Ref: seriesRef, Token: token})
		if err != nil {
			return err
		}
		return render.ApplyReconcile(rt.Stdout, result, cmd.JSON)
	})
}

func splitReconcileApplyArgs(args []string) ([]string, string, error) {
	if len(args) < 2 {
		return nil, "", errors.New("reconcile apply requires at least one selector term and a plan token")
	}
	return args[:len(args)-1], args[len(args)-1], nil
}

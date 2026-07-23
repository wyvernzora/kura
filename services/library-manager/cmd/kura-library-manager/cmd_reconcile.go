package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/services/library-manager/internal/cli/render"
	"github.com/wyvernzora/kura/services/library-manager/internal/cli/stdio"
	"github.com/wyvernzora/kura/services/library-manager/internal/reconcile"
	"github.com/wyvernzora/kura/services/library-manager/internal/response"
)

type reconcileCmd struct {
	Plan    reconcilePlanCmd    `cmd:"" help:"Create a reconcile plan for a tracked series."`
	Apply   reconcileApplyCmd   `cmd:"" help:"Apply a saved reconcile plan for a tracked series."`
	Recover reconcileRecoverCmd `cmd:"" help:"Clear a stale in_progress claim from a series's series.json."`
}

type reconcilePlanCmd struct {
	JSON  bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Terms []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

type reconcileApplyCmd struct {
	JSON bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Args []string `arg:"" required:"" help:"Resolver terms followed by the reconcile plan token."`
}

type reconcileRecoverCmd struct {
	JSON    bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Force   bool     `name:"force" help:"Break the claim regardless of holder identity. Required for cross-host claims."`
	Confirm bool     `name:"confirm" help:"Required gate when --force is set."`
	Terms   []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *reconcilePlanCmd) Run(rt *runContext) error {
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, cmd.Terms)
	if err != nil {
		return err
	}
	result, err := c.ReconcilePlan(rt.Context, ref)
	if err != nil {
		return err
	}
	return render.PlanReconcile(rt.Stdout, result, cmd.JSON)
}

func (cmd *reconcileApplyCmd) Run(rt *runContext) error {
	terms, token, err := splitReconcileApplyArgs(cmd.Args)
	if err != nil {
		return err
	}
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, terms)
	if err != nil {
		return err
	}
	ack, err := c.SubmitApply(rt.Context, ref, token)
	if err != nil {
		return err
	}
	raw, err := waitForJobResult(rt, c, ack.JobID)
	if err != nil {
		var applyResult reconcile.ApplyResult
		if raw != nil {
			_ = json.Unmarshal(raw, &applyResult)
		}
		_ = render.ApplyReconcile(rt.Stderr, applyResultToResponse(applyResult), cmd.JSON)
		return err
	}
	var applyResult reconcile.ApplyResult
	if err := json.Unmarshal(raw, &applyResult); err != nil {
		return fmt.Errorf("decode reconcile apply result: %w", err)
	}
	return render.ApplyReconcile(rt.Stdout, applyResultToResponse(applyResult), cmd.JSON)
}

// applyResultToResponse mirrors workflow.ApplyReconcileResponse so the
// CLI can render the wire shape without importing workflow. The job
// registry stores the raw reconcile.ApplyResult JSON; CLI converts to
// response.ReconcileApply for render.
func applyResultToResponse(result reconcile.ApplyResult) response.ReconcileApply {
	out := response.ReconcileApply{
		Series:         result.Series,
		AppliedSteps:   result.AppliedSteps,
		TotalSteps:     result.TotalSteps,
		AppliedStepIDs: append([]string(nil), result.AppliedStepIDs...),
	}
	if result.FailedStep != nil {
		fs := *result.FailedStep
		out.FailedStep = &response.FailedReconcileStep{
			ID:         fs.ID,
			Kind:       string(fs.Kind),
			OwnerKind:  string(fs.OwnerKind),
			From:       fs.From,
			To:         fs.To,
			Path:       fs.Path,
			ErrMessage: fs.ErrMessage,
		}
	}
	return out
}

func splitReconcileApplyArgs(args []string) (terms []string, planToken string, err error) {
	if len(args) < 2 {
		return nil, "", errors.New("reconcile apply requires at least one selector term and a plan token")
	}
	return args[:len(args)-1], args[len(args)-1], nil
}

func (cmd *reconcileRecoverCmd) Run(rt *runContext) error {
	if cmd.Force && !cmd.Confirm {
		return errors.New("reconcile recover --force requires --confirm")
	}
	c := operatorClientFromRT(rt)
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, cmd.Terms)
	if err != nil {
		return err
	}
	result, err := c.ReconcileRecover(rt.Context, ref, cmd.Force)
	if err != nil {
		return err
	}
	return render.RecoverReconcile(rt.Stdout, result, cmd.JSON)
}

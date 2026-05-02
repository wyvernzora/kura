package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/internal/series"
	"github.com/wyvernzora/kura/internal/ui"
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

type reconcilePlanOutput struct {
	Token     string               `json:"token,omitempty"`
	CreatedAt string               `json:"createdAt,omitempty"`
	ExpiresAt string               `json:"expiresAt,omitempty"`
	Plan      series.ReconcilePlan `json:"plan"`
}

func (cmd *reconcilePlanCmd) Run(rt *runContext) error {
	handle, err := resolveSeriesHandle(rt, cmd.Terms)
	if err != nil {
		return err
	}
	stored, err := handle.CreateReconcilePlan()
	if err != nil {
		return err
	}
	if cmd.JSON {
		encoder := json.NewEncoder(rt.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(reconcilePlanOutput{
			Token:     stored.Token,
			CreatedAt: formatOptionalTime(stored.CreatedAt),
			ExpiresAt: formatOptionalTime(stored.ExpiresAt),
			Plan:      stored.Plan,
		})
	}
	if err := ui.WriteReconcilePlan(rt.Stdout, stored.Plan); err != nil {
		return err
	}
	if stored.Token == "" {
		return nil
	}
	_, err = fmt.Fprintf(rt.Stdout, "Token: %s\nExpiresAt: %s\n", stored.Token, stored.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"))
	return err
}

func (cmd *reconcileApplyCmd) Run(rt *runContext) error {
	terms, token, err := splitReconcileApplyArgs(cmd.Args)
	if err != nil {
		return err
	}
	handle, err := resolveSeriesHandle(rt, terms)
	if err != nil {
		return err
	}
	result, err := handle.ApplyReconcileToken(rt.Context, token)
	if err != nil {
		return err
	}
	if cmd.JSON {
		encoder := json.NewEncoder(rt.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	_, err = fmt.Fprintf(rt.Stdout, "Applied %d reconcile moves for %s\n", result.AppliedMoves, result.Series)
	return err
}

func splitReconcileApplyArgs(args []string) ([]string, string, error) {
	if len(args) < 2 {
		return nil, "", errors.New("reconcile apply requires at least one selector term and a plan token")
	}
	return args[:len(args)-1], args[len(args)-1], nil
}

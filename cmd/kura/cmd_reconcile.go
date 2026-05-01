package main

import (
	"encoding/json"

	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/ui"
)

type seriesReconcileCmd struct {
	DryRun bool   `name:"dry-run" help:"Print planned changes without renaming files or writing metadata."`
	JSON   bool   `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Yes    bool   `name:"yes" short:"y" help:"Apply planned changes without prompting."`
	Series string `arg:"" help:"Series selector. Currently resolves as a directory name below KURA_LIBRARY_ROOT."`
}

func (cmd *seriesReconcileCmd) Run(rt *runContext) error {
	lib, err := libraryFromFlags(rt, rt.flags)
	if err != nil {
		return err
	}
	ref, err := refs.ParseSeries(cmd.Series)
	if err != nil {
		return err
	}
	handle, err := lib.Open(ref)
	if err != nil {
		return err
	}
	plan, err := handle.PlanReconcile()
	if err != nil {
		return err
	}

	if !plan.HasChanges() {
		if cmd.JSON {
			encoder := json.NewEncoder(rt.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(plan)
		}
		return ui.WriteReconcilePlan(rt.Stdout, plan)
	}
	if cmd.JSON {
		encoder := json.NewEncoder(rt.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(plan); err != nil {
			return err
		}
	} else if err := ui.WriteReconcilePlan(rt.Stdout, plan); err != nil {
		return err
	}
	if cmd.DryRun {
		return nil
	}
	if !cmd.Yes {
		confirmed, err := ui.Confirm(rt.Stdin, rt.Stderr, "Apply these changes? [y/N] ")
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}
	_, err = handle.ApplyReconcile(rt.Context, plan)
	return err
}

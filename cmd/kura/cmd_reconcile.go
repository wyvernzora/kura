package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/ops"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/store"
	"github.com/wyvernzora/kura/internal/ui"
)

type seriesReconcileCmd struct {
	DryRun bool   `name:"dry-run" help:"Print planned changes without renaming files or writing metadata."`
	JSON   bool   `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Yes    bool   `name:"yes" short:"y" help:"Apply planned changes without prompting."`
	Series string `arg:"" help:"Series selector. Currently resolves as a directory name below KURA_LIBRARY_ROOT."`
}

func (cmd *seriesReconcileCmd) Run(rt runContext) error {
	root, err := fsroot.ParseLibraryRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}
	seriesDir, dirErr := root.SeriesDir(cmd.Series)
	plan, err := ops.PlanSeries(rt.Context, root, cmd.Series)
	if err != nil {
		if dirErr == nil {
			warnDuplicateSeries(rt, seriesDir.Path(), err)
		}
		return err
	}
	plan.DryRun = cmd.DryRun

	if cmd.JSON {
		encoder := json.NewEncoder(rt.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(plan); err != nil {
			return err
		}
	} else if err := ui.WriteReconcilePlan(rt.Stdout, plan); err != nil {
		return err
	}
	if cmd.DryRun || !plan.HasChanges() {
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
	return ops.ApplyPlan(
		progress.With(rt.Context, ui.NewProgressReporter(rt.Stderr)),
		plan,
	)
}

func warnDuplicateSeries(rt runContext, seriesDir string, err error) {
	if !errors.Is(err, store.DuplicateEpisodeNumberError{}) {
		return
	}
	fmt.Fprintf(rt.Stderr, "warning: %s contains duplicate episode entries; manually edit series.json before continuing\n", store.SeriesPath(seriesDir))
}

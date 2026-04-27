package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/terminalui"
)

type seriesReconcileCmd struct {
	DryRun bool   `name:"dry-run" help:"Print planned changes without renaming files or writing metadata."`
	JSON   bool   `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Yes    bool   `name:"yes" short:"y" help:"Apply planned changes without prompting."`
	Series string `arg:"" help:"Series selector. Currently resolves as a directory name below KURA_LIBRARY_ROOT."`
}

func (cmd *seriesReconcileCmd) Run(rt runContext) error {
	lib := library.New()
	root, err := library.ParseLibraryRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}
	seriesDir, dirErr := resolveSeriesSelector(root, cmd.Series)
	plan, err := lib.PlanReconcile(rt.Context, root, cmd.Series)
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
	} else if err := terminalui.WriteReconcilePlan(rt.Stdout, plan); err != nil {
		return err
	}
	if cmd.DryRun || !plan.HasChanges() {
		return nil
	}
	if !cmd.Yes {
		confirmed, err := terminalui.Confirm(rt.Stdin, rt.Stderr, "Apply these changes? [y/N] ")
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}
	return lib.ApplyReconcile(
		library.WithProgress(rt.Context, terminalui.NewProgressReporter(rt.Stderr)),
		plan,
	)
}

func warnDuplicateSeries(rt runContext, seriesDir string, err error) {
	if !errors.Is(err, library.DuplicateEpisodeNumberError{}) {
		return
	}
	fmt.Fprintf(rt.Stderr, "warning: %s contains duplicate episode entries; manually edit series.json before continuing\n", library.SeriesPath(seriesDir))
}

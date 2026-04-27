package main

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/terminalui"
)

type seriesCmd struct {
	Sync      seriesSyncCmd      `cmd:"" help:"Scan a series directory and sync it into Kura metadata."`
	Reconcile seriesReconcileCmd `cmd:"" help:"Rename tracked files and the series directory to match Kura metadata."`
}

type seriesSyncCmd struct {
	Provider    string `help:"Metadata provider to use when searching." enum:"tvdb" default:"tvdb"`
	ProviderRef string `name:"provider-ref" help:"Provider series reference, such as tvdb:370070. Bypasses search."`
	TVDBBaseURL string `name:"tvdb-base-url" hidden:"" help:"Override the TVDB API base URL."`
	DryRun      bool   `name:"dry-run" help:"Print planned changes without writing series metadata."`
	JSON        bool   `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Yes         bool   `name:"yes" short:"y" help:"Apply planned changes without prompting."`
	Dirname     string `arg:"" help:"Series directory name below KURA_LIBRARY_ROOT."`
}

func (cmd *seriesSyncCmd) Run(rt runContext) error {
	lib := library.New()
	root, err := library.ParseLibraryRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}
	seriesDir, err := root.SeriesDir(cmd.Dirname)
	if err != nil {
		return err
	}

	var providerSeries *metadata.Series
	if _, err := os.Stat(library.SeriesPath(seriesDir.Path())); errors.Is(err, os.ErrNotExist) {
		resolved, _, err := cmd.resolveProviderSeries(rt)
		if err != nil {
			return err
		}
		providerSeries = &resolved
	} else if err != nil {
		return err
	}

	result, err := lib.SyncSeries(
		library.WithProgress(rt.Context, terminalui.NewProgressReporter(rt.Stderr)),
		root,
		cmd.Dirname,
		library.SeriesSyncOptions{
			ProviderSeries: providerSeries,
			Inspector:      mediaInspector(rt),
			DryRun:         cmd.DryRun,
		},
	)
	if err != nil {
		return err
	}
	if cmd.JSON {
		encoder := json.NewEncoder(rt.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return err
		}
	} else if err := terminalui.WriteSeriesSyncResult(rt.Stdout, result); err != nil {
		return err
	}

	if cmd.DryRun || !result.HasChanges() {
		return nil
	}
	if !cmd.Yes {
		confirmed, err := terminalui.Confirm(rt.Stdin, rt.Stderr, "Apply this sync? [y/N] ")
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}
	progress := terminalui.NewProgress(rt.Stderr)
	progress.Start("Writing series metadata: %s", library.SeriesPath(seriesDir.Path()))
	defer progress.Stop()
	if err := lib.SaveSeries(result.UpdatedSeries); err != nil {
		progress.Fail("Failed writing series metadata")
		return err
	}
	progress.Succeed("Synced %d episode media file(s)", len(result.Synced))
	return nil
}

type seriesReconcileCmd struct {
	DryRun  bool   `name:"dry-run" help:"Print planned changes without renaming files or writing metadata."`
	JSON    bool   `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Yes     bool   `name:"yes" short:"y" help:"Apply planned changes without prompting."`
	Dirname string `arg:"" help:"Series directory name below KURA_LIBRARY_ROOT."`
}

func (cmd *seriesReconcileCmd) Run(rt runContext) error {
	lib := library.New()
	root, err := library.ParseLibraryRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}
	seriesDir, dirErr := root.SeriesDir(cmd.Dirname)
	plan, err := lib.PlanReconcile(rt.Context, root, cmd.Dirname)
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

func (cmd *seriesSyncCmd) resolveProviderSeries(rt runContext) (metadata.Series, bool, error) {
	metadataSource, err := buildMetadataSource(rt, cmd.Provider, cmd.TVDBBaseURL)
	if err != nil {
		return metadata.Series{}, false, err
	}
	resolved, selected, err := library.ResolveProviderSeries(rt.Context, metadataSource, cmd.Dirname, library.ResolveSeriesOptions{
		ProviderRef: cmd.ProviderRef,
		SearchLimit: 5,
	})
	if err != nil {
		selectionRequired, ok := errors.AsType[library.SeriesSelectionRequiredError](err)
		if !ok || !isInteractiveRun(rt) {
			return metadata.Series{}, false, err
		}
		stdin := rt.Stdin.(*os.File)
		stdout := rt.Stdout.(*os.File)
		match, ok, selectErr := terminalui.SelectSeriesCandidate(stdin, stdout, rt.Stderr, cmd.Dirname, selectionRequired.Candidates)
		if selectErr != nil {
			return metadata.Series{}, false, selectErr
		}
		if !ok {
			return metadata.Series{}, false, err
		}
		resolved, err = library.GetProviderSeriesByRef(rt.Context, metadataSource, match.ProviderRef)
		if err != nil {
			return metadata.Series{}, false, err
		}
		selected = true
	}
	return resolved, selected, nil
}

func isInteractiveRun(rt runContext) bool {
	stdin, stdinOK := rt.Stdin.(*os.File)
	stdout, stdoutOK := rt.Stdout.(*os.File)
	return stdinOK && stdoutOK && terminalui.IsTerminal(stdin) && terminalui.IsTerminal(stdout)
}

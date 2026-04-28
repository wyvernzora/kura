package main

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/ops"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/resolve"
	"github.com/wyvernzora/kura/internal/store"
	"github.com/wyvernzora/kura/internal/ui"
)

type seriesSyncCmd struct {
	Provider    string `help:"Metadata provider to use when searching." enum:"tvdb" default:"tvdb"`
	ProviderRef string `name:"provider-ref" help:"Provider series reference, such as tvdb:370070. Bypasses search."`
	TVDBBaseURL string `name:"tvdb-base-url" hidden:"" help:"Override the TVDB API base URL."`
	DryRun      bool   `name:"dry-run" help:"Print planned changes without writing series metadata."`
	JSON        bool   `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Yes         bool   `name:"yes" short:"y" help:"Apply planned changes without prompting."`
	Replace     bool   `name:"replace" help:"Replace existing episode records, moving old records to trash."`
	Series      string `arg:"" help:"Series selector. Currently resolves as a directory name below KURA_LIBRARY_ROOT."`
}

func (cmd *seriesSyncCmd) Run(rt runContext) error {
	root, err := fsroot.ParseLibraryRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}
	seriesDir, err := root.SeriesDir(cmd.Series)
	if err != nil {
		return err
	}

	var providerSeries *metadata.Series
	if _, err := os.Stat(store.SeriesPath(seriesDir.Path())); errors.Is(err, os.ErrNotExist) {
		resolved, _, err := cmd.resolveProviderSeries(rt)
		if err != nil {
			return err
		}
		providerSeries = &resolved
	} else if err != nil {
		return err
	}

	result, err := ops.SyncSeries(
		progress.With(rt.Context, ui.NewProgressReporter(rt.Stderr)),
		root,
		cmd.Series,
		ops.SeriesSyncOptions{
			ProviderSeries:   providerSeries,
			ProviderResolver: providerSeriesResolver(rt, cmd.Provider, cmd.TVDBBaseURL),
			Inspector:        mediaInspector(rt),
			DryRun:           cmd.DryRun,
			Replace:          cmd.Replace,
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
	} else if err := ui.WriteSeriesSyncResult(rt.Stdout, result); err != nil {
		return err
	}

	if cmd.DryRun || !result.HasChanges() {
		return nil
	}
	if !cmd.Yes {
		confirmed, err := ui.Confirm(rt.Stdin, rt.Stderr, "Apply this sync? [y/N] ")
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}
	progress := ui.NewProgress(rt.Stderr)
	progress.Start("Writing series metadata: %s", store.SeriesPath(seriesDir.Path()))
	defer progress.Stop()
	if err := store.SaveSeries(result.UpdatedSeries); err != nil {
		progress.Fail("Failed writing series metadata")
		return err
	}
	if err := store.SaveTrash(result.UpdatedTrash); err != nil {
		progress.Fail("Failed writing trash metadata")
		return err
	}
	progress.Succeed("Synced %d episode media file(s)", len(result.Synced))
	return nil
}

func (cmd *seriesSyncCmd) resolveProviderSeries(rt runContext) (metadata.Series, bool, error) {
	metadataSource, err := buildMetadataSource(rt, cmd.Provider, cmd.TVDBBaseURL)
	if err != nil {
		return metadata.Series{}, false, err
	}
	resolved, selected, err := resolve.ResolveProviderSeries(rt.Context, metadataSource, cmd.Series, resolve.ResolveSeriesOptions{
		ProviderRef: cmd.ProviderRef,
		SearchLimit: 5,
	})
	if err != nil {
		selectionRequired, ok := errors.AsType[resolve.SeriesSelectionRequiredError](err)
		if !ok || !isInteractiveRun(rt) {
			return metadata.Series{}, false, err
		}
		stdin := rt.Stdin.(*os.File)
		stdout := rt.Stdout.(*os.File)
		match, ok, selectErr := ui.SelectSeriesCandidate(stdin, stdout, rt.Stderr, cmd.Series, selectionRequired.Candidates)
		if selectErr != nil {
			return metadata.Series{}, false, selectErr
		}
		if !ok {
			return metadata.Series{}, false, err
		}
		resolved, err = resolve.GetProviderSeriesByRef(rt.Context, metadataSource, match.ProviderRef)
		if err != nil {
			return metadata.Series{}, false, err
		}
		selected = true
	}
	return resolved, selected, nil
}

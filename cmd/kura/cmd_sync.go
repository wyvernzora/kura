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
	MetadataRef string `name:"metadata-ref" help:"Metadata series reference, such as tvdb:370070. Bypasses search."`
	DryRun      bool   `name:"dry-run" help:"Print planned changes without writing series metadata."`
	JSON        bool   `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Yes         bool   `name:"yes" short:"y" help:"Apply planned changes without prompting."`
	Replace     bool   `name:"replace" help:"Replace existing episode records, moving old records to trash."`
	Series      string `arg:"" help:"Series selector. Currently resolves as a directory name below KURA_LIBRARY_ROOT."`
}

func (cmd *seriesSyncCmd) Run(rt *runContext) error {
	root, err := fsroot.ParseLibraryRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}
	seriesDir, err := root.SeriesDir(cmd.Series)
	if err != nil {
		return err
	}

	var metadataSeries *metadata.Series
	if _, err := os.Stat(store.SeriesPath(seriesDir.Path())); errors.Is(err, os.ErrNotExist) {
		resolved, _, err := cmd.resolveMetadataSeries(rt)
		if err != nil {
			return err
		}
		metadataSeries = &resolved
	} else if err != nil {
		return err
	}

	result, err := ops.SyncSeries(
		progress.With(rt.Context, ui.NewProgressReporter(rt.Stderr)),
		root,
		cmd.Series,
		ops.SeriesSyncOptions{
			MetadataSeries:   metadataSeries,
			MetadataResolver: metadataSeriesResolver(rt),
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

func (cmd *seriesSyncCmd) resolveMetadataSeries(rt *runContext) (metadata.Series, bool, error) {
	metadataSource, err := metadata.SourceFrom(rt.Context)
	if err != nil {
		return metadata.Series{}, false, err
	}
	resolved, selected, err := resolve.ResolveMetadataSeries(rt.Context, metadataSource, cmd.Series, resolve.ResolveSeriesOptions{
		MetadataRef: cmd.MetadataRef,
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
		resolved, err = resolve.GetMetadataSeriesByRef(rt.Context, metadataSource, match.MetadataRef)
		if err != nil {
			return metadata.Series{}, false, err
		}
		selected = true
	}
	return resolved, selected, nil
}

package main

import (
	"encoding/json"
	"fmt"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/ops"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/store"
	"github.com/wyvernzora/kura/internal/ui"
)

type scanCmd struct {
	DryRun  bool     `name:"dry-run" help:"Print planned changes without writing series metadata."`
	JSON    bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Yes     bool     `name:"yes" short:"y" help:"Apply planned changes without prompting."`
	Replace bool     `name:"replace" help:"Replace existing episode records, moving old records to trash."`
	Terms   []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *scanCmd) Run(rt *runContext) error {
	root, err := fsroot.ParseLibraryRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}
	meta, err := ui.ResolveSeries(rt.Context, cmd.Terms)
	if err != nil {
		return err
	}
	ref, err := domain.ParseMetadataRef(meta.MetadataRef)
	if err != nil {
		return err
	}
	index, err := store.LibraryIndexFrom(rt.Context)
	if err != nil {
		return err
	}
	path, ok, err := index.Get(ref)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("library: no tracked series for %s; run kura import or kura add", ref)
	}
	seriesDir, err := root.SeriesDir(path.String())
	if err != nil {
		return err
	}

	result, err := ops.SyncSeries(
		progress.With(rt.Context, ui.NewProgressReporter(rt.Stderr)),
		root,
		path.String(),
		ops.SeriesSyncOptions{
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
		confirmed, err := ui.Confirm(rt.Stdin, rt.Stderr, "Apply this scan? [y/N] ")
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}
	progress := ui.NewProgress(rt.Stderr)
	progress.Start("Writing series metadata: %s", store.SeriesMetadataPath(seriesDir.Path()))
	defer progress.Stop()
	if err := store.SaveSeries(result.UpdatedSeries); err != nil {
		progress.Fail("Failed writing series metadata")
		return err
	}
	if err := store.SaveTrash(result.UpdatedTrash); err != nil {
		progress.Fail("Failed writing trash metadata")
		return err
	}
	if err := updateLibraryIndex(rt, result.UpdatedSeries, path); err != nil {
		progress.Fail("Failed writing library index")
		return err
	}
	progress.Succeed("Scanned %d episode media file(s)", len(result.Synced))
	return nil
}

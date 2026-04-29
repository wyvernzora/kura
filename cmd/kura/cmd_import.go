package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/ops"
	"github.com/wyvernzora/kura/internal/store"
	"github.com/wyvernzora/kura/internal/ui"
)

type importCmd struct {
	Dirname string   `arg:"" required:"" help:"Existing directory below KURA_LIBRARY_ROOT."`
	JSON    bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Terms   []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *importCmd) Run(rt *runContext) error {
	root, err := fsroot.ParseLibraryRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}
	seriesDir, err := root.SeriesDir(cmd.Dirname)
	if err != nil {
		return err
	}
	if _, err := os.Stat(store.SeriesMetadataPath(seriesDir.Path())); err == nil {
		return fmt.Errorf("library: %q already has .kura/series.json; use kura scan instead", cmd.Dirname)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	meta, err := ui.ResolveSeries(rt.Context, cmd.Terms)
	if err != nil {
		return err
	}
	seriesPath, err := domain.ParseSeriesPath(seriesDir.Name())
	if err != nil {
		return err
	}
	if err := assertMetadataRefAvailable(rt, meta.MetadataRef, seriesPath); err != nil {
		return err
	}
	result, err := ops.InitSeries(ops.InitSeriesOptions{SeriesDir: seriesDir, Metadata: meta})
	if err != nil {
		return err
	}
	if err := store.SaveSeries(result.Series); err != nil {
		return err
	}
	if err := updateLibraryIndex(rt, result.Series, result.SeriesPath); err != nil {
		return err
	}
	return cmd.writeSummary(rt, result)
}

func (cmd *importCmd) writeSummary(rt *runContext, result ops.InitSeriesResult) error {
	if cmd.JSON {
		encoder := json.NewEncoder(rt.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result.Series)
	}
	_, err := fmt.Fprintf(rt.Stdout, "Imported %s (%s)\n", result.SeriesPath, result.Series.MetadataRef)
	return err
}

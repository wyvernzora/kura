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

type addCmd struct {
	Dirname string   `name:"dirname" help:"Directory name override; defaults to preferred title."`
	JSON    bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Terms   []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *addCmd) Run(rt *runContext) error {
	root, err := fsroot.ParseLibraryRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}

	meta, err := ui.ResolveSeries(rt.Context, cmd.Terms)
	if err != nil {
		return err
	}

	dirnameRaw := cmd.Dirname
	if dirnameRaw == "" {
		dirnameRaw = meta.PreferredTitle
	}
	fileTitle, err := domain.ParseFileTitle(dirnameRaw)
	if err != nil {
		return err
	}
	dirname := fileTitle.String()
	seriesPath, err := domain.ParseSeriesPath(dirname)
	if err != nil {
		return err
	}

	target := root.Join(dirname)
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("library: %q already exists below the library root", dirname)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := assertMetadataRefAvailable(rt, meta.MetadataRef, seriesPath); err != nil {
		return err
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	seriesDir, err := root.SeriesDir(dirname)
	if err != nil {
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
	return cmd.writeSummary(rt, result, dirname)
}

func (cmd *addCmd) writeSummary(rt *runContext, result ops.InitSeriesResult, dirname string) error {
	if cmd.JSON {
		encoder := json.NewEncoder(rt.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result.Series)
	}
	_, err := fmt.Fprintf(rt.Stdout, "Added %s (%s)\n", dirname, result.Series.MetadataRef)
	return err
}

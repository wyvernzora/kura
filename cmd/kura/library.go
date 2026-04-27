package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/internal/library"
)

type libraryCmd struct {
	AddEpisode libraryAddEpisodeCmd `cmd:"" name:"add-episode" help:"Record an existing episode file in .kura/series.json."`
}

type libraryAddEpisodeCmd struct {
	Season  int    `help:"Season number. Use 0 for specials."`
	Episode int    `help:"Episode number."`
	File    string `help:"Media file path relative to the series folder."`
	DryRun  bool   `name:"dry-run" help:"Print the updated series document without writing it."`
	Replace bool   `name:"replace" help:"Replace an existing episode record, moving the old record to trash."`
	Path    string `arg:"" help:"Existing series folder path."`
}

func (cmd *libraryAddEpisodeCmd) Run(rt runContext) error {
	lib := library.New()
	if cmd.File == "" {
		return errors.New("--file is required")
	}
	seriesDir, err := cleanSeriesDir(cmd.Path)
	if err != nil {
		return err
	}

	series, err := loadSeries(rt, lib, seriesDir)
	if err != nil {
		return err
	}
	trash, err := lib.LoadTrash(seriesDir)
	if err != nil {
		return err
	}
	updated, err := library.AddEpisode(seriesDir, *series, library.AddEpisodeOptions{
		Season:  cmd.Season,
		Episode: cmd.Episode,
		Path:    cmd.File,
		Replace: cmd.Replace,
		Trash:   trash,
	})
	if err != nil {
		return err
	}
	if !cmd.DryRun {
		if err := lib.SaveSeries(updated); err != nil {
			return err
		}
		if err := lib.SaveTrash(*trash); err != nil {
			return err
		}
	}

	encoder := json.NewEncoder(rt.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(updated)
}

func loadSeries(rt runContext, lib library.Library, seriesDir string) (*library.Series, error) {
	series, err := lib.LoadSeries(seriesDir)
	if err != nil {
		warnDuplicateSeries(rt, seriesDir, err)
		return nil, err
	}
	return series, nil
}

func warnDuplicateSeries(rt runContext, seriesDir string, err error) {
	if _, ok := errors.AsType[library.DuplicateEpisodeNumberError](err); ok {
		fmt.Fprintf(rt.Stderr, "warning: %s contains duplicate episode entries; manually edit series.json before continuing\n", library.SeriesPath(seriesDir))
	}
}

func cleanSeriesDir(path string) (string, error) {
	seriesDir, err := library.ParseSeriesDir(path)
	if err != nil {
		return "", err
	}
	return seriesDir.Path(), nil
}

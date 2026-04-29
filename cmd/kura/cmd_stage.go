package main

import (
	"encoding/json"
	"errors"

	"github.com/wyvernzora/kura/internal/kura"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/ui"
)

type stageCmd struct {
	Season     int      `help:"Season number."`
	Special    bool     `help:"Stage as a special."`
	Number     int      `help:"Episode number." required:""`
	Source     string   `help:"Media source. Defaults to filename source or unknown."`
	Companions []string `name:"companion" help:"Absolute companion file path."`
	Replace    bool     `name:"replace" help:"Stage over an active episode or replace an existing staged entry for the same season and episode."`
	Series     string   `arg:"" help:"Series selector. Currently resolves as a directory name below KURA_LIBRARY_ROOT."`
	Path       string   `arg:"" help:"Absolute media file path to stage."`
}

func (cmd *stageCmd) Run(rt *runContext) error {
	if cmd.Special && cmd.Season != 0 {
		return errors.New("--season and --special are mutually exclusive")
	}
	if !cmd.Special && cmd.Season < 1 {
		return errors.New("--season is required unless --special is set")
	}

	season := 0
	if !cmd.Special {
		season = cmd.Season
	}
	if cmd.Number < 1 {
		return errors.New("--number must be greater than zero")
	}
	lib, err := libraryFromFlags(rt, rt.flags)
	if err != nil {
		return err
	}
	series, err := lib.Get(kura.SeriesRef(cmd.Series))
	if err != nil {
		return err
	}

	result, err := series.Stage(
		progress.With(rt.Context, ui.NewProgressReporter(rt.Stderr)),
		kura.StageInput{
			Season:     season,
			Episode:    cmd.Number,
			Source:     cmd.Source,
			Companions: cmd.Companions,
			MediaPath:  cmd.Path,
			Replace:    cmd.Replace,
		},
	)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(rt.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

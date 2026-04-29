package main

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/ops"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/ui"
)

type stageCmd struct {
	Season     int      `help:"Season number."`
	Special    bool     `help:"Stage as a special."`
	Number     int      `help:"Episode number." required:""`
	Source     string   `help:"Media source. Defaults to filename source or unknown."`
	Companions []string `name:"companion" help:"Absolute companion file path."`
	DryRun     bool     `name:"dry-run" help:"Print the updated staged document without writing it."`
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

	season := domain.SpecialsSeason()
	var err error
	if !cmd.Special {
		season, err = domain.RegularSeason(cmd.Season)
		if err != nil {
			return err
		}
	}
	episode, err := domain.NewEpisodeNumber(cmd.Number)
	if err != nil {
		return errors.New("--number must be greater than zero")
	}
	root, err := fsroot.ParseLibraryRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}

	source := domain.MediaSource("")
	if strings.TrimSpace(cmd.Source) != "" {
		source = domain.ParseMediaSource(cmd.Source)
	}
	result, err := ops.StageEpisodeFile(
		progress.With(rt.Context, ui.NewProgressReporter(rt.Stderr)),
		root,
		cmd.Series,
		ops.StageEpisodeFileOptions{
			Season:           season,
			Episode:          episode,
			Source:           source,
			Companions:       cmd.Companions,
			MediaPath:        cmd.Path,
			Inspector:        mediaInspector(rt),
			MetadataResolver: metadataSeriesResolver(rt),
			Apply:            !cmd.DryRun,
			Replace:          cmd.Replace,
		},
	)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(rt.Stdout)
	encoder.SetIndent("", "  ")
	if cmd.DryRun {
		return encoder.Encode(result.UpdatedStaged)
	}
	return encoder.Encode(result)
}

package main

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/terminalui"
)

type episodeCmd struct {
	Import episodeImportCmd `cmd:"" help:"Import an existing episode file into .kura/series.json."`
}

type episodeImportCmd struct {
	Season     int      `help:"Season number."`
	Special    bool     `help:"Import as a special."`
	Number     int      `help:"Episode number." required:""`
	Source     string   `help:"Media source. Defaults to filename source or unknown."`
	Companions []string `name:"companion" help:"Companion file path relative to KURA_LIBRARY_ROOT."`
	DryRun     bool     `name:"dry-run" help:"Print the updated series document without writing it."`
	Path       string   `arg:"" help:"Media file path relative to KURA_LIBRARY_ROOT."`
}

func (cmd *episodeImportCmd) Run(rt runContext) error {
	if cmd.Special && cmd.Season != 0 {
		return errors.New("--season and --special are mutually exclusive")
	}
	if !cmd.Special && cmd.Season < 1 {
		return errors.New("--season is required unless --special is set")
	}

	season := library.SpecialsSeason()
	var err error
	if !cmd.Special {
		season, err = library.RegularSeason(cmd.Season)
		if err != nil {
			return err
		}
	}
	episode, err := library.NewEpisodeNumber(cmd.Number)
	if err != nil {
		return errors.New("--number must be greater than zero")
	}
	root, err := library.ParseLibraryRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}

	source := library.MediaSource("")
	if strings.TrimSpace(cmd.Source) != "" {
		source = library.ParseMediaSource(cmd.Source)
	}
	updated, err := library.New().ImportEpisodeFile(
		library.WithProgress(rt.Context, terminalui.NewProgressReporter(rt.Stderr)),
		root,
		library.ImportEpisodeFileOptions{
			Season:     season,
			Episode:    episode,
			Source:     source,
			Companions: cmd.Companions,
			MediaPath:  cmd.Path,
			Inspector:  mediaInspector(rt),
			Apply:      !cmd.DryRun,
		},
	)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(rt.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(updated)
}

func mediaInspector(rt runContext) mediainfo.Inspector {
	inspector := mediainfo.New()
	command := strings.TrimSpace(rt.Getenv("KURA_MEDIAINFO_COMMAND"))
	if command != "" {
		inspector.Command = command
	}
	return inspector
}

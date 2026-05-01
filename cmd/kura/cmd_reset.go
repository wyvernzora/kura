package main

import (
	"encoding/json"
	"errors"

	"github.com/wyvernzora/kura/internal/series"
)

type resetCmd struct {
	Episode string   `name:"episode" help:"Episode marker or ref, e.g. S01E03 or S01E0003."`
	All     bool     `name:"all" help:"Remove every staged record for the series."`
	Terms   []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *resetCmd) Run(rt *runContext) error {
	input := series.ResetInput{All: cmd.All}
	if cmd.Episode == "" && !cmd.All {
		return errors.New("reset requires --episode or --all")
	}
	if cmd.Episode != "" && cmd.All {
		return errors.New("reset accepts either --episode or --all, not both")
	}
	if cmd.Episode != "" {
		episode, err := parseStageEpisode(cmd.Episode)
		if err != nil {
			return err
		}
		input.Episode = episode
	}
	lib, err := libraryFromFlags(rt, rt.flags)
	if err != nil {
		return err
	}
	metadataRef, err := resolveMetadataRef(rt, lib, cmd.Terms)
	if err != nil {
		return err
	}
	handle, err := lib.Find(metadataRef)
	if err != nil {
		return err
	}
	result, err := handle.Reset(rt.Context, input)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(rt.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

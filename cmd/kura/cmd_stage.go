package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series"
)

type stageCmd struct {
	Episode    string   `name:"episode" help:"Episode marker or ref, e.g. S01E03 or S01E0003." required:""`
	Source     string   `help:"Media source. Defaults to filename source or unknown."`
	Companions []string `name:"companion" help:"Companion file path. Relative paths resolve from the series root."`
	Replace    bool     `name:"replace" help:"Stage over an active episode or replace an existing staged entry for the same season and episode."`
	Args       []string `arg:"" required:"" help:"Selector terms followed by the media file path to stage."`
}

func (cmd *stageCmd) Run(rt *runContext) error {
	terms, mediaPath, err := splitStageArgs(cmd.Args)
	if err != nil {
		return err
	}
	episode, err := parseStageEpisode(cmd.Episode)
	if err != nil {
		return err
	}
	lib, err := libraryFromFlags(rt, rt.flags)
	if err != nil {
		return err
	}
	metadataRef, err := resolveMetadataRef(rt, lib, terms)
	if err != nil {
		return err
	}
	handle, err := lib.Find(metadataRef)
	if err != nil {
		return err
	}
	view, err := handle.Read(rt.Context, series.ReadInput{})
	if err != nil {
		return err
	}
	mediaPath = pathFromSeriesRoot(view.Root, mediaPath)
	companions := make([]string, 0, len(cmd.Companions))
	for _, companion := range cmd.Companions {
		companions = append(companions, pathFromSeriesRoot(view.Root, companion))
	}

	result, err := handle.Stage(rt.Context, series.StageInput{
		Episode:    episode,
		Source:     cmd.Source,
		Companions: companions,
		MediaPath:  mediaPath,
		Replace:    cmd.Replace,
	})
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(rt.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func splitStageArgs(args []string) ([]string, string, error) {
	if len(args) < 2 {
		return nil, "", errors.New("stage requires at least one selector term and a media path")
	}
	return args[:len(args)-1], args[len(args)-1], nil
}

var stageEpisodePattern = regexp.MustCompile(`^S([0-9]{2,})E([0-9]{2,})$`)

func parseStageEpisode(value string) (refs.Episode, error) {
	ref, err := refs.ParseEpisode(value)
	if err == nil {
		return ref, nil
	}
	match := stageEpisodePattern.FindStringSubmatch(value)
	if match == nil {
		return refs.Episode{}, fmt.Errorf("invalid episode %q; expected marker S01E03 or episode ref S01E0003", value)
	}
	season, err := strconv.Atoi(match[1])
	if err != nil {
		return refs.Episode{}, err
	}
	episode, err := strconv.Atoi(match[2])
	if err != nil {
		return refs.Episode{}, err
	}
	return refs.NewEpisode(season, episode)
}

func pathFromSeriesRoot(seriesRoot string, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(seriesRoot, filepath.FromSlash(path))
}

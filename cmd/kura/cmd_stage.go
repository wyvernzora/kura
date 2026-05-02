package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"

	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/workflow"
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
	deps, err := buildDeps(rt)
	if err != nil {
		return err
	}
	io := stdio.From(rt.Context)
	return clipkg.WithResolve(rt.Context, io, deps, terms, func(metadataRef refs.Metadata) error {
		seriesRef, ok, err := deps.Index.Get(metadataRef)
		if err != nil {
			return err
		}
		if !ok {
			return &workflow.MetadataRefNotIndexedError{Ref: metadataRef}
		}
		seriesRoot := paths.SeriesDir(deps.LibRoot, seriesRef)
		absoluteMedia := pathFromSeriesRoot(seriesRoot, mediaPath)
		companions := make([]string, 0, len(cmd.Companions))
		for _, c := range cmd.Companions {
			companions = append(companions, pathFromSeriesRoot(seriesRoot, c))
		}
		result, err := workflow.Stage(rt.Context, deps, workflow.StageInput{
			Ref:            seriesRef,
			Episode:        episode,
			Source:         cmd.Source,
			CompanionPaths: companions,
			MediaPath:      absoluteMedia,
			Replace:        cmd.Replace,
		})
		if err != nil {
			return err
		}
		return render.Stage(rt.Stdout, result, true)
	})
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

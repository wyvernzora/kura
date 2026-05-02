package main

import (
	"errors"

	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/workflow"
)

type resetCmd struct {
	Episode string   `name:"episode" help:"Episode marker or ref, e.g. S01E03 or S01E0003."`
	All     bool     `name:"all" help:"Remove every staged record for the series."`
	Terms   []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *resetCmd) Run(rt *runContext) error {
	if cmd.Episode == "" && !cmd.All {
		return errors.New("reset requires --episode or --all")
	}
	if cmd.Episode != "" && cmd.All {
		return errors.New("reset accepts either --episode or --all, not both")
	}
	in := workflow.ResetInput{All: cmd.All}
	if cmd.Episode != "" {
		episode, err := parseStageEpisode(cmd.Episode)
		if err != nil {
			return err
		}
		in.Episode = episode
	}
	deps, err := buildDeps(rt)
	if err != nil {
		return err
	}
	io := stdio.From(rt.Context)
	return clipkg.WithResolve(rt.Context, io, deps, cmd.Terms, func(metadataRef refs.Metadata) error {
		seriesRef, ok, err := deps.Index.Get(metadataRef)
		if err != nil {
			return err
		}
		if !ok {
			return &workflow.MetadataRefNotIndexedError{Ref: metadataRef}
		}
		in.Ref = seriesRef
		result, err := workflow.Reset(rt.Context, deps, in)
		if err != nil {
			return err
		}
		return render.Reset(rt.Stdout, result, true)
	})
}

package main

import (
	"errors"

	"github.com/oklog/ulid/v2"

	"github.com/wyvernzora/kura/internal/cli/client"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

type resetCmd struct {
	Episode string   `name:"episode" help:"Episode marker or ref, e.g. S01E03 or S01E0003."`
	Trash   []string `name:"trash" help:"ULID of a stagedTrash entry to drop. May be repeated."`
	Extra   []string `name:"extra" help:"ULID of a stagedExtras entry to drop. May be repeated."`
	All     bool     `name:"all" help:"Remove every staged record (episodes + trash + extras) for the series."`
	JSON    bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Terms   []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *resetCmd) Run(rt *runContext) error {
	if cmd.Episode == "" && !cmd.All && len(cmd.Trash) == 0 && len(cmd.Extra) == 0 {
		return errors.New("reset requires --episode, --trash, --extra, or --all")
	}
	if cmd.All && (cmd.Episode != "" || len(cmd.Trash) > 0 || len(cmd.Extra) > 0) {
		return errors.New("reset --all is mutually exclusive with --episode/--trash/--extra")
	}
	req := client.ResetRequest{All: cmd.All}
	if cmd.Episode != "" {
		ep, err := refs.ParseEpisodeMarker(cmd.Episode)
		if err != nil {
			return err
		}
		req.Episode = ep.String()
	}
	for _, raw := range cmd.Trash {
		if _, err := ulid.Parse(raw); err != nil {
			return err
		}
		req.TrashIDs = append(req.TrashIDs, raw)
	}
	for _, raw := range cmd.Extra {
		if _, err := ulid.Parse(raw); err != nil {
			return err
		}
		req.ExtraIDs = append(req.ExtraIDs, raw)
	}
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, cmd.Terms)
	if err != nil {
		return err
	}
	result, err := c.ResetSeries(rt.Context, ref, req)
	if err != nil {
		return err
	}
	return render.Reset(rt.Stdout, result, cmd.JSON)
}

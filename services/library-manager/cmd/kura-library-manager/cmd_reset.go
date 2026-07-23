package main

import (
	"errors"

	"github.com/oklog/ulid/v2"

	"github.com/wyvernzora/kura/services/library-manager/internal/cli/client"
	"github.com/wyvernzora/kura/services/library-manager/internal/cli/render"
	"github.com/wyvernzora/kura/services/library-manager/internal/cli/stdio"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
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
	if err := validateResetCmdFlags(cmd); err != nil {
		return err
	}
	req, err := buildResetCmdRequest(cmd)
	if err != nil {
		return err
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

// validateResetCmdFlags enforces the "at least one mode" + "all is
// mutually exclusive" rules over the CLI flag set.
func validateResetCmdFlags(cmd *resetCmd) error {
	if cmd.Episode == "" && !cmd.All && len(cmd.Trash) == 0 && len(cmd.Extra) == 0 {
		return errors.New("reset requires --episode, --trash, --extra, or --all")
	}
	if cmd.All && (cmd.Episode != "" || len(cmd.Trash) > 0 || len(cmd.Extra) > 0) {
		return errors.New("reset --all is mutually exclusive with --episode/--trash/--extra")
	}
	return nil
}

// buildResetCmdRequest parses the episode marker + ULID lists into a
// client.ResetRequest. Returns the underlying parser error verbatim
// so the CLI surface preserves the original message wording.
func buildResetCmdRequest(cmd *resetCmd) (client.ResetRequest, error) {
	req := client.ResetRequest{All: cmd.All}
	if cmd.Episode != "" {
		ep, err := refs.ParseEpisodeMarker(cmd.Episode)
		if err != nil {
			return client.ResetRequest{}, err
		}
		req.Episode = ep.String()
	}
	for _, raw := range cmd.Trash {
		if _, err := ulid.Parse(raw); err != nil {
			return client.ResetRequest{}, err
		}
		req.TrashIDs = append(req.TrashIDs, raw)
	}
	for _, raw := range cmd.Extra {
		if _, err := ulid.Parse(raw); err != nil {
			return client.ResetRequest{}, err
		}
		req.ExtraIDs = append(req.ExtraIDs, raw)
	}
	return req, nil
}

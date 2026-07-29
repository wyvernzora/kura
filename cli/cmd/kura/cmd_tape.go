package main

import (
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/cli/internal/cli/client"
	"github.com/wyvernzora/kura/cli/internal/cli/render"
	tapeapi "github.com/wyvernzora/kura/services/tape-backup/pkg/api"
)

type tapeCmd struct {
	Status  tapeStatusCmd  `cmd:"" help:"Show the persisted consultation, live roster, and drive state."`
	Consult tapeConsultCmd `cmd:"" help:"Size pending work and mint the known-tape draft roster."`
	Plan    tapePlanCmd    `cmd:"" help:"Preview a plan for the loaded cartridge."`
	Run     tapeRunCmd     `cmd:"" help:"Run the validated plan for the loaded cartridge."`
	Approve tapeApproveCmd `cmd:"" help:"Approve an init plan's blank-cartridge attestation."`
	Discard tapeDiscardCmd `cmd:"" help:"Discard a draft or ready plan."`
}

type tapeStatusCmd struct {
	JSON bool `name:"json" help:"Print machine-readable JSON instead of a human summary."`
}

type tapeConsultCmd struct {
	JSON   bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Blanks []string `name:"blank" placeholder:"ID" help:"Declare a blank cartridge for sizing; repeat for multiple cartridges."`
}

type tapePlanCmd struct {
	JSON   bool   `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	TapeID string `arg:"" optional:"" name:"tape" help:"Declared label for unidentified media; omit for identified media."`
}

type tapeRunCmd struct {
	JSON   bool   `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	TapeID string `arg:"" optional:"" name:"tape" help:"Declared label for unidentified media; omit for identified media."`
}

type tapeApproveCmd struct {
	PlanID string `arg:"" required:"" name:"plan" help:"Init plan ID to approve."`
}

type tapeDiscardCmd struct {
	PlanID string `arg:"" required:"" name:"plan" help:"Draft or ready plan ID to discard."`
}

func (cmd *tapeStatusCmd) Run(rt *runContext) error {
	result, err := clientFromRT(rt).TapeStatus(rt.Context)
	if err != nil {
		return tapeError(err)
	}
	return render.TapeStatus(rt.Stdout, result, cmd.JSON)
}

func (cmd *tapeConsultCmd) Run(rt *runContext) error {
	blanks := make([]tapeapi.Blank, 0, len(cmd.Blanks))
	for _, tapeID := range cmd.Blanks {
		blanks = append(blanks, tapeapi.Blank{TapeID: tapeID})
	}
	result, err := clientFromRT(rt).TapeConsult(rt.Context, blanks)
	if err != nil {
		return tapeError(err)
	}
	return render.TapeConsult(rt.Stdout, result, cmd.JSON)
}

func (cmd *tapePlanCmd) Run(rt *runContext) error {
	result, err := clientFromRT(rt).TapePlan(
		rt.Context,
		tapeapi.PlanRequest{TapeID: cmd.TapeID},
	)
	if err != nil {
		return tapeError(err)
	}
	return render.TapePlan(rt.Stdout, result, cmd.JSON)
}

func (cmd *tapeRunCmd) Run(rt *runContext) error {
	result, err := clientFromRT(rt).TapeRun(
		rt.Context,
		tapeapi.PlanRequest{TapeID: cmd.TapeID},
	)
	if err != nil {
		return tapeError(err)
	}
	return render.TapeRun(rt.Stdout, result, cmd.JSON)
}

func (cmd *tapeApproveCmd) Run(rt *runContext) error {
	if err := clientFromRT(rt).TapeApprove(rt.Context, cmd.PlanID); err != nil {
		return tapeError(err)
	}
	_, err := fmt.Fprintf(rt.Stdout, "Approved %s.\n", cmd.PlanID)
	return err
}

func (cmd *tapeDiscardCmd) Run(rt *runContext) error {
	if err := clientFromRT(rt).TapeDiscard(rt.Context, cmd.PlanID); err != nil {
		return tapeError(err)
	}
	_, err := fmt.Fprintf(rt.Stdout, "Discarded %s.\n", cmd.PlanID)
	return err
}

func tapeError(err error) error {
	var envelope *client.ErrorEnvelope
	if !errors.As(err, &envelope) {
		return err
	}
	if envelope.Message == "" {
		return fmt.Errorf("%s", envelope.Kind)
	}
	return fmt.Errorf("%s: %s", envelope.Kind, envelope.Message)
}

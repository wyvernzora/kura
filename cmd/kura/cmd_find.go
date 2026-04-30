package main

import (
	"encoding/json"

	"github.com/wyvernzora/kura/internal/series"
	"github.com/wyvernzora/kura/internal/ui"
)

type findCmd struct {
	JSON  bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Terms []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *findCmd) Run(rt *runContext) error {
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
	result, err := handle.Read(rt.Context, series.ReadInput{})
	if err != nil {
		return err
	}
	if cmd.JSON {
		encoder := json.NewEncoder(rt.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	return ui.WriteSeriesRead(rt.Stdout, result)
}

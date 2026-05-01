package main

import (
	"encoding/json"

	"github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/ui"
)

type listCmd struct {
	JSON bool `name:"json" help:"Print machine-readable JSON instead of a human summary."`
}

func (cmd *listCmd) Run(rt *runContext) error {
	entries, err := library.List(rt.Context, library.ListInput{
		Root: rt.Getenv("KURA_LIBRARY_ROOT"),
	})
	if err != nil {
		return err
	}
	if cmd.JSON {
		encoder := json.NewEncoder(rt.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(entries)
	}
	return ui.WriteLibraryList(rt.Stdout, entries)
}

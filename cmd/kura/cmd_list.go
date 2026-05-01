package main

import (
	"encoding/json"

	"github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/ui"
)

type listCmd struct {
	JSON     bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Statuses []string `name:"status" help:"Only show entries with this status. Repeat for multiple statuses."`
}

func (cmd *listCmd) Run(rt *runContext) error {
	statuses := make([]library.ListStatus, 0, len(cmd.Statuses))
	for _, raw := range cmd.Statuses {
		status, err := library.ParseListStatus(raw)
		if err != nil {
			return err
		}
		statuses = append(statuses, status)
	}
	entries, err := library.List(rt.Context, library.ListInput{
		Root:     rt.Getenv("KURA_LIBRARY_ROOT"),
		Statuses: statuses,
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

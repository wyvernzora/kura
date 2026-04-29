package main

import (
	"encoding/json"
	"fmt"

	"github.com/wyvernzora/kura/internal/kura"
)

type importCmd struct {
	Dirname string   `arg:"" required:"" help:"Existing directory below KURA_LIBRARY_ROOT."`
	JSON    bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Terms   []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *importCmd) Run(rt *runContext) error {
	lib, err := libraryFromFlags(rt, rt.flags)
	if err != nil {
		return err
	}
	metadataRef, err := resolveMetadataRef(rt, lib, cmd.Terms)
	if err != nil {
		return err
	}
	series, err := lib.Import(rt.Context, kura.ImportInput{
		Ref:         kura.SeriesRef(cmd.Dirname),
		MetadataRef: metadataRef,
	})
	if err != nil {
		return err
	}
	return cmd.writeSummary(rt, series)
}

func (cmd *importCmd) writeSummary(rt *runContext, series *kura.Series) error {
	if cmd.JSON {
		encoder := json.NewEncoder(rt.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(series)
	}
	_, err := fmt.Fprintf(rt.Stdout, "Imported %s (%s)\n", series.Ref(), series.MetadataRef())
	return err
}

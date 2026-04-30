package main

import (
	"encoding/json"

	"github.com/wyvernzora/kura/internal/kura"
	"github.com/wyvernzora/kura/internal/ui"
)

type scanCmd struct {
	JSON    bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Replace bool     `name:"replace" help:"Replace existing episode records, moving old records to trash."`
	Terms   []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *scanCmd) Run(rt *runContext) error {
	lib, err := libraryFromFlags(rt, rt.flags)
	if err != nil {
		return err
	}
	metadataRef, err := resolveMetadataRef(rt, lib, cmd.Terms)
	if err != nil {
		return err
	}
	series, err := lib.Find(metadataRef)
	if err != nil {
		return err
	}
	result, err := series.Scan(rt.Context, kura.ScanInput{Replace: cmd.Replace})
	if err != nil {
		return err
	}
	if cmd.JSON {
		encoder := json.NewEncoder(rt.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return err
		}
	} else if err := ui.WriteScanResult(rt.Stdout, result); err != nil {
		return err
	}
	return nil
}

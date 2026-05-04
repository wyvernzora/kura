package main

import (
	"encoding/json"
	"fmt"

	"github.com/wyvernzora/kura/internal/cli/client"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/provider/tvdb"
	"github.com/wyvernzora/kura/internal/response"
)

type scanCmd struct {
	JSON     bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Refresh  bool     `name:"refresh" help:"Force re-run of mediainfo and source detection on every active record, even when size and mtime are unchanged. A freshly detected Unknown source will not overwrite an existing non-Unknown one."`
	Ordering string   `name:"ordering" help:"Pin the per-series episode ordering and re-fetch the spine under it. One of: default, official, dvd, absolute, alternate, regional. Omit to keep the series's existing ordering."`
	Terms    []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *scanCmd) Run(rt *runContext) error {
	ordering, err := tvdb.ParseOrdering(cmd.Ordering)
	if err != nil {
		return err
	}
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, cmd.Terms)
	if err != nil {
		return err
	}
	ack, err := c.SubmitScan(rt.Context, ref, client.ScanRequest{Refresh: cmd.Refresh, Ordering: ordering})
	if err != nil {
		return err
	}
	raw, err := waitForJobResult(rt, c, ack.JobID)
	if err != nil {
		return err
	}
	var result response.ScanResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode scan result: %w", err)
	}
	return render.Scan(rt.Stdout, result, cmd.JSON)
}

// waitForJobResult streams the job to terminal and returns its result
// JSON. Surfaces server-side errors as ErrorEnvelope. Used by every
// async CLI verb (scan, stage, reconcile apply).
func waitForJobResult(rt *runContext, c *client.Client, jobID string) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.StreamJob(rt.Context, jobID, func(ev client.JobEvent) {
		if ev.Kind == "result" {
			result = ev.Result
		}
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

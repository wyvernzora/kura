package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/services/library-manager/internal/cli/client"
	"github.com/wyvernzora/kura/services/library-manager/internal/cli/render"
	"github.com/wyvernzora/kura/services/library-manager/internal/cli/stdio"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider/tvdb"
	"github.com/wyvernzora/kura/services/library-manager/internal/response"
)

type scanCmd struct {
	JSON         bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Refresh      bool     `name:"refresh" help:"Force re-run of mediainfo and source detection on every active record, even when size and mtime are unchanged. A freshly detected Unknown source will not overwrite an existing non-Unknown one."`
	MetadataOnly bool     `name:"metadata-only" help:"Skip the filesystem walk + mediainfo probes. Refreshes provider spine + artwork + alias data and rewrites searchKey; active records left untouched. Useful for cheap library-wide refreshes after a metadata-shape change."`
	Ordering     string   `name:"ordering" help:"Pin the per-series episode ordering and re-fetch the spine under it. One of: default, official, dvd, absolute, alternate, regional. Omit to keep the series's existing ordering. Ignored when --all is set."`
	All          bool     `name:"all" help:"Rescan every tracked (non-untracked) series in the library via the server-side scan-all job. Mutually exclusive with positional Terms and --ordering."`
	Concurrency  int      `name:"concurrency" help:"Override the server-side scan-all worker pool size. Defaults to 4."`
	Terms        []string `arg:"" optional:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070. Omit when --all is set."`
}

func (cmd *scanCmd) Run(rt *runContext) error {
	if cmd.All && len(cmd.Terms) > 0 {
		return errors.New("scan --all is mutually exclusive with positional terms")
	}
	if cmd.All && cmd.Ordering != "" {
		return errors.New("scan --all is mutually exclusive with --ordering")
	}
	if !cmd.All && len(cmd.Terms) == 0 {
		return errors.New("scan requires terms or --all")
	}
	c := clientFromRT(rt)
	if cmd.All {
		return cmd.runAll(rt, c)
	}
	ordering, err := tvdb.ParseOrdering(cmd.Ordering)
	if err != nil {
		return err
	}
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, cmd.Terms)
	if err != nil {
		return err
	}
	ack, err := c.SubmitScan(rt.Context, ref, client.ScanRequest{
		Refresh:      cmd.Refresh,
		MetadataOnly: cmd.MetadataOnly,
		Ordering:     ordering,
	})
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

// runAll dispatches the library-wide scan to the server-side scan-all
// job and renders aggregate progress + summary. The server emits one
// progress event per per-series completion (current/total = ref.String()),
// which the reindex-style live status line consumes verbatim.
func (cmd *scanCmd) runAll(rt *runContext, c *client.Client) error {
	io := stdio.From(rt.Context)
	ack, err := c.SubmitScanAll(rt.Context, client.ScanAllRequest{
		Refresh:      cmd.Refresh,
		MetadataOnly: cmd.MetadataOnly,
		Concurrency:  cmd.Concurrency,
	})
	if err != nil {
		return err
	}

	progress := newReindexProgress(io.OutIsTTY, rt.Stdout)
	defer progress.stop()

	var resultRaw json.RawMessage
	streamErr := c.StreamJob(rt.Context, ack.JobID, func(ev client.JobEvent) {
		switch ev.Kind {
		case "progress":
			if ev.Progress != nil {
				progress.update(ev.Progress.Message, ev.Progress.Current, ev.Progress.Total)
			}
		case "result":
			resultRaw = ev.Result
		}
	})
	progress.stop()
	if streamErr != nil {
		return streamErr
	}
	var result response.ScanAllResult
	if len(resultRaw) > 0 {
		if err := json.Unmarshal(resultRaw, &result); err != nil {
			return fmt.Errorf("decode scan-all result: %w", err)
		}
	}
	if err := render.ScanAll(rt.Stdout, result, cmd.JSON); err != nil {
		return err
	}
	if result.Failed > 0 {
		return fmt.Errorf("scan --all: %d series failed", result.Failed)
	}
	return nil
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

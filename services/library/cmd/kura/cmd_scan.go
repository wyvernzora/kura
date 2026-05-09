package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/wyvernzora/kura/internal/cli/client"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/provider/tvdb"
	"github.com/wyvernzora/kura/internal/response"
)

// allScanConcurrency caps the in-flight scan jobs when --all is set.
// Four keeps a comfortable margin under TVDB's unwritten RPS ceiling
// while still parallelizing the bulk of wall-clock on a typical
// library. Tunable via --concurrency if a future TVDB tier supports
// more.
const allScanConcurrency = 4

type scanCmd struct {
	JSON         bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Refresh      bool     `name:"refresh" help:"Force re-run of mediainfo and source detection on every active record, even when size and mtime are unchanged. A freshly detected Unknown source will not overwrite an existing non-Unknown one."`
	MetadataOnly bool     `name:"metadata-only" help:"Skip the filesystem walk + mediainfo probes. Refreshes provider spine + artwork + alias data and rewrites searchKey; active records left untouched. Useful for cheap library-wide refreshes after a metadata-shape change."`
	Ordering     string   `name:"ordering" help:"Pin the per-series episode ordering and re-fetch the spine under it. One of: default, official, dvd, absolute, alternate, regional. Omit to keep the series's existing ordering."`
	All          bool     `name:"all" help:"Rescan every tracked (non-untracked, non-error) series in the library, four at a time. Mutually exclusive with positional Terms."`
	Concurrency  int      `name:"concurrency" help:"Override --all worker pool size. Defaults to 4."`
	Terms        []string `arg:"" optional:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070. Omit when --all is set."`
}

func (cmd *scanCmd) Run(rt *runContext) error {
	if cmd.All && len(cmd.Terms) > 0 {
		return errors.New("scan --all is mutually exclusive with positional terms")
	}
	if !cmd.All && len(cmd.Terms) == 0 {
		return errors.New("scan requires terms or --all")
	}
	ordering, err := tvdb.ParseOrdering(cmd.Ordering)
	if err != nil {
		return err
	}
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	if cmd.All {
		return cmd.runAll(rt, c, ordering)
	}
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

// runAll fans out one SubmitScan + waitForJobResult per tracked
// series, capped at the --concurrency pool.
//
// Progress display:
//   - TTY stdout: live single-line status redrawn every ~150 ms
//     ("[N/M] X in flight · Y ok · Z failed · current: ref1, ref2")
//     plus FAIL lines preserved on stderr above the live line.
//   - non-TTY: per-completion ok / fail lines, one per ref. Avoids
//     spamming \r control bytes into log files / piped output.
func (cmd *scanCmd) runAll(rt *runContext, c *client.Client, ordering string) error {
	refs, err := listScannableRefs(rt.Context, c)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		fmt.Fprintln(rt.Stdout, "No tracked series in the library.")
		return nil
	}
	workers := cmd.Concurrency
	if workers <= 0 {
		workers = allScanConcurrency
	}
	if workers > len(refs) {
		workers = len(refs)
	}

	req := client.ScanRequest{
		Refresh:      cmd.Refresh,
		MetadataOnly: cmd.MetadataOnly,
		Ordering:     ordering,
	}

	io := stdio.From(rt.Context)
	tty := io.OutIsTTY
	tracker := newScanProgress(int64(len(refs)), tty, rt.Stdout, rt.Stderr)
	tracker.start()
	defer tracker.stop()

	// errgroup carries gctx so a user Ctrl-C stops dispatching further
	// refs at the next Acquire; in-flight scans cancel via rt.Context
	// inside scanOne. Per-ref errors are owned by the tracker (FAIL
	// lines printed above the live status), so goroutines return nil
	// and we ignore g.Wait()'s error.
	g, gctx := errgroup.WithContext(rt.Context)
	sem := semaphore.NewWeighted(int64(workers))
	for _, ref := range refs {
		if err := sem.Acquire(gctx, 1); err != nil {
			break
		}
		g.Go(func() error {
			defer sem.Release(1)
			tracker.startOne(ref)
			err := cmd.scanOne(rt, c, ref, req)
			tracker.finishOne(ref, err)
			return nil
		})
	}
	_ = g.Wait()

	ok, failed := tracker.totals()
	tracker.stop()
	fmt.Fprintf(rt.Stdout, "--- done: %d ok, %d failed ---\n", ok, failed)
	if failed > 0 {
		return fmt.Errorf("scan --all: %d series failed", failed)
	}
	return nil
}

// scanProgress tracks ok / fail / in-flight counts for `kura scan
// --all` and renders them. On a TTY it owns a single status line
// that's redrawn at refreshInterval; on a non-TTY it falls back to
// one log line per completion.
type scanProgress struct {
	total int64
	tty   bool
	out   io.Writer
	err   io.Writer

	ok   atomic.Int64
	fail atomic.Int64
	done atomic.Int64

	mu       sync.Mutex // guards inFlight + line render
	inFlight []string
	lastLen  int

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func newScanProgress(total int64, tty bool, out, errOut io.Writer) *scanProgress {
	return &scanProgress{
		total:  total,
		tty:    tty,
		out:    out,
		err:    errOut,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

func (p *scanProgress) start() {
	if !p.tty {
		close(p.doneCh)
		return
	}
	go p.loop()
}

func (p *scanProgress) loop() {
	defer close(p.doneCh)
	t := time.NewTicker(150 * time.Millisecond)
	defer t.Stop()
	p.render()
	for {
		select {
		case <-p.stopCh:
			return
		case <-t.C:
			p.render()
		}
	}
}

func (p *scanProgress) startOne(ref string) {
	p.mu.Lock()
	p.inFlight = append(p.inFlight, ref)
	p.mu.Unlock()
}

func (p *scanProgress) finishOne(ref string, err error) {
	p.mu.Lock()
	for i, r := range p.inFlight {
		if r == ref {
			p.inFlight = append(p.inFlight[:i], p.inFlight[i+1:]...)
			break
		}
	}
	p.mu.Unlock()
	idx := p.done.Add(1)
	if err != nil {
		p.fail.Add(1)
		// FAIL lines are durable: print them above the live status
		// line on a TTY, or as a normal stderr line otherwise.
		p.printAbove(p.err, fmt.Sprintf("[%d/%d] FAIL %s: %v\n", idx, p.total, ref, err))
	} else {
		p.ok.Add(1)
		if !p.tty {
			fmt.Fprintf(p.out, "[%d/%d]   ok %s\n", idx, p.total, ref)
		}
	}
	if p.tty {
		p.render()
	}
}

func (p *scanProgress) totals() (ok, failed int64) {
	return p.ok.Load(), p.fail.Load()
}

func (p *scanProgress) stop() {
	p.stopOnce.Do(func() {
		close(p.stopCh)
		<-p.doneCh
		if p.tty {
			p.clearLine()
		}
	})
}

// printAbove writes msg to w. On a TTY it first clears the live
// status line so the durable message lands cleanly above the
// re-rendered status. Caller already holds no locks.
func (p *scanProgress) printAbove(w io.Writer, msg string) {
	if p.tty {
		p.mu.Lock()
		p.eraseLocked()
		fmt.Fprint(w, msg)
		p.lastLen = 0
		p.mu.Unlock()
		return
	}
	fmt.Fprint(w, msg)
}

// render redraws the single-line status. Builds the line from the
// current snapshot of counters + in-flight slice, then writes via
// `\r` overwrite. No-op on non-TTY (start() never spawns the loop).
func (p *scanProgress) render() {
	p.mu.Lock()
	defer p.mu.Unlock()
	ok := p.ok.Load()
	failed := p.fail.Load()
	done := p.done.Load()
	flight := strings.Join(p.inFlight, ", ")
	if len(flight) > 60 {
		flight = flight[:57] + "..."
	}
	line := fmt.Sprintf("[%d/%d] %d in flight · %d ok · %d failed",
		done, p.total, len(p.inFlight), ok, failed)
	if flight != "" {
		line += " · " + flight
	}
	p.eraseLocked()
	fmt.Fprint(p.out, "\r"+line)
	p.lastLen = len(line)
}

// eraseLocked overwrites the previously-rendered status line with
// spaces and rewinds. Caller must hold p.mu.
func (p *scanProgress) eraseLocked() {
	if p.lastLen == 0 {
		return
	}
	fmt.Fprint(p.out, "\r"+strings.Repeat(" ", p.lastLen)+"\r")
}

// clearLine erases the live status line on shutdown so the final
// summary doesn't share the row.
func (p *scanProgress) clearLine() {
	p.mu.Lock()
	p.eraseLocked()
	p.lastLen = 0
	p.mu.Unlock()
}

// scanOne submits a scan for a single ref and blocks on its job
// stream until terminal. Used by --all's worker pool.
func (cmd *scanCmd) scanOne(rt *runContext, c *client.Client, ref string, req client.ScanRequest) error {
	ack, err := c.SubmitScan(rt.Context, ref, req)
	if err != nil {
		return err
	}
	if _, err := waitForJobResult(rt, c, ack.JobID); err != nil {
		return err
	}
	return nil
}

// listScannableRefs walks `kura list`'s cursor pagination, returning
// every metadataRef with a status worth scanning (skip untracked +
// error rows). Mirrors the gating tools/scripts/rescan-library.sh
// previously did via jq.
func listScannableRefs(ctx context.Context, c *client.Client) ([]string, error) {
	var (
		refs   []string
		cursor string
	)
	for {
		page, err := c.ListSeries(ctx, nil, nil, 0, cursor)
		if err != nil {
			return nil, err
		}
		for _, row := range page.Rows {
			ref := string(row.MetadataRef)
			if ref == "" {
				continue
			}
			switch row.Status {
			case response.ListStatusUntracked, response.ListStatusError:
				continue
			}
			refs = append(refs, ref)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return refs, nil
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

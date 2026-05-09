package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/wyvernzora/kura/internal/cli/client"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/response"
)

type reindexCmd struct {
	JSON bool `name:"json" help:"Print the resulting row count as JSON instead of a human summary."`
}

func (cmd *reindexCmd) Run(rt *runContext) error {
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	ack, err := c.SubmitReindex(rt.Context)
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
	var result response.ReindexResult
	if len(resultRaw) > 0 {
		if err := json.Unmarshal(resultRaw, &result); err != nil {
			return fmt.Errorf("decode reindex result: %w", err)
		}
	}
	if cmd.JSON {
		enc := json.NewEncoder(rt.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	fmt.Fprintf(rt.Stdout, "reindex done: %d rows\n", result.Rows)
	return nil
}

// reindexProgress renders progress events from `kura reindex`. On a
// TTY it owns a single `\r`-overwritten status line; on non-TTY it
// stays silent (no per-update spam in piped logs).
type reindexProgress struct {
	tty bool
	out io.Writer

	mu      sync.Mutex
	lastLen int
	stopped bool
}

func newReindexProgress(tty bool, out io.Writer) *reindexProgress {
	return &reindexProgress{tty: tty, out: out}
}

func (p *reindexProgress) update(message string, current, total int) {
	if !p.tty {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	line := message
	if line == "" {
		line = "rebuilding library index"
	}
	if total > 0 {
		line = fmt.Sprintf("[%d/%d] %s", current, total, line)
	} else if current > 0 {
		line = fmt.Sprintf("[%d] %s", current, line)
	}
	p.eraseLocked()
	fmt.Fprint(p.out, "\r"+line)
	p.lastLen = len(line)
}

func (p *reindexProgress) stop() {
	if !p.tty {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.eraseLocked()
	p.lastLen = 0
	p.stopped = true
}

func (p *reindexProgress) eraseLocked() {
	if p.lastLen == 0 {
		return
	}
	fmt.Fprint(p.out, "\r"+strings.Repeat(" ", p.lastLen)+"\r")
}

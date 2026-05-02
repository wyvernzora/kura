package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/ui/stdio"
)

type spinnerProgress struct {
	enabled bool
	spinner *spinner.Spinner
	stderr  io.Writer
}

func newSpinnerProgress(stderr io.Writer) *spinnerProgress {
	file, ok := stderr.(*os.File)
	if !ok || !stdio.IsTerminal(file) {
		return &spinnerProgress{}
	}
	s := spinner.New(spinner.CharSets[11], 100*time.Millisecond, spinner.WithWriter(stderr))
	s.FinalMSG = ""
	return &spinnerProgress{enabled: true, spinner: s, stderr: stderr}
}

func newProgressReporter(stderr io.Writer) progress.Reporter {
	sp := newSpinnerProgress(stderr)
	return func(_ context.Context, event progress.Event) {
		message := progressMessage(event)
		switch event.Status {
		case progress.StartStatus:
			sp.start("%s", message)
		case progress.UpdateStatus:
			sp.update("%s", message)
		case progress.SuccessStatus:
			sp.succeed("%s", message)
		case progress.FailureStatus:
			sp.fail("%s", message)
		}
	}
}

func progressMessage(event progress.Event) string {
	if event.Total <= 0 || event.Current <= 0 {
		return event.Message
	}
	return fmt.Sprintf("[%d/%d] %s", event.Current, event.Total, event.Message)
}

func (p *spinnerProgress) start(format string, args ...any) {
	if !p.enabled {
		return
	}
	p.spinner.Suffix = " " + fmt.Sprintf(format, args...)
	p.spinner.Start()
}

func (p *spinnerProgress) update(format string, args ...any) {
	if !p.enabled {
		return
	}
	p.spinner.Suffix = " " + fmt.Sprintf(format, args...)
}

func (p *spinnerProgress) succeed(format string, args ...any) {
	if !p.enabled {
		return
	}
	p.spinner.Stop()
	fmt.Fprintf(p.stderr, "✔ %s\n", fmt.Sprintf(format, args...))
}

func (p *spinnerProgress) fail(format string, args ...any) {
	if !p.enabled {
		return
	}
	p.spinner.Stop()
	fmt.Fprintf(p.stderr, "✖ %s\n", fmt.Sprintf(format, args...))
}

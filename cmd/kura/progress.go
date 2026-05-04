package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/progress"
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
	// WithWriterFile (vs WithWriter) sets the *os.File the spinner
	// uses for its internal isatty check. Without it the lib defaults
	// to os.Stdout for the check and silently disables the spinner
	// whenever stdout is piped, even though we write the spinner to
	// stderr.
	s := spinner.New(spinner.CharSets[11], 100*time.Millisecond, spinner.WithWriterFile(file))
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
	if event.Current <= 0 {
		return event.Message
	}
	switch {
	case event.Total > 0:
		return fmt.Sprintf("[%d/%d] %s", event.Current, event.Total, event.Message)
	case event.Total == progress.TotalIndeterminate:
		return fmt.Sprintf("[%d/?] %s", event.Current, event.Message)
	default:
		return event.Message
	}
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

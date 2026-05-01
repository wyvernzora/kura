package ui

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

type Progress struct {
	enabled bool
	spinner *spinner.Spinner
	stderr  io.Writer
}

func NewProgress(stderr io.Writer) *Progress {
	file, ok := stderr.(*os.File)
	if !ok || !stdio.IsTerminal(file) {
		return &Progress{}
	}
	s := spinner.New(spinner.CharSets[11], 100*time.Millisecond, spinner.WithWriter(stderr))
	s.FinalMSG = ""
	return &Progress{enabled: true, spinner: s, stderr: stderr}
}

func NewProgressReporter(stderr io.Writer) progress.Reporter {
	spinnerProgress := NewProgress(stderr)
	return func(_ context.Context, event progress.Event) {
		message := progressMessage(event)
		switch event.Status {
		case progress.StartStatus:
			spinnerProgress.Start("%s", message)
		case progress.UpdateStatus:
			spinnerProgress.Update("%s", message)
		case progress.SuccessStatus:
			spinnerProgress.Succeed("%s", message)
		case progress.FailureStatus:
			spinnerProgress.Fail("%s", message)
		}
	}
}

func progressMessage(event progress.Event) string {
	if event.Total <= 0 || event.Current <= 0 {
		return event.Message
	}
	return fmt.Sprintf("[%d/%d] %s", event.Current, event.Total, event.Message)
}

func (p *Progress) Start(format string, args ...any) {
	if !p.enabled {
		return
	}
	p.spinner.Suffix = " " + fmt.Sprintf(format, args...)
	p.spinner.Start()
}

func (p *Progress) Update(format string, args ...any) {
	if !p.enabled {
		return
	}
	p.spinner.Suffix = " " + fmt.Sprintf(format, args...)
}

func (p *Progress) Succeed(format string, args ...any) {
	if !p.enabled {
		return
	}
	p.spinner.Stop()
	fmt.Fprintf(p.stderr, "✔ %s\n", fmt.Sprintf(format, args...))
}

func (p *Progress) Fail(format string, args ...any) {
	if !p.enabled {
		return
	}
	p.spinner.Stop()
	fmt.Fprintf(p.stderr, "✖ %s\n", fmt.Sprintf(format, args...))
}

func (p *Progress) Stop() {
	if !p.enabled {
		return
	}
	p.spinner.Stop()
}

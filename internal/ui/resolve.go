package ui

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/AlecAivazis/survey/v2"
	"github.com/wyvernzora/kura/internal/resolve"
	"github.com/wyvernzora/kura/internal/ui/stdio"
)

var (
	ErrNoMetadataMatch     = errors.New("ui: no metadata match")
	ErrSelectionRequired   = errors.New("ui: metadata selection required")
	ErrSelectionCancelled  = errors.New("ui: metadata selection cancelled")
	errInteractiveRequires = errors.New("ui: interactive metadata selection requires file stdio")
)

func SelectFromResolution(io stdio.Stdio, resolution resolve.Resolution, terms []string) (resolve.Result, error) {
	switch len(resolution.Results) {
	case 0:
		return resolve.Result{}, fmt.Errorf("%w for %v", ErrNoMetadataMatch, terms)
	case 1:
		return resolution.Results[0], nil
	}

	if !io.IsInteractive() {
		writeCandidatesHint(io.Err, resolution.Results)
		return resolve.Result{}, ErrSelectionRequired
	}
	picked, ok, err := selectResolveCandidate(io, terms, resolution.Results)
	if err != nil {
		return resolve.Result{}, err
	}
	if !ok {
		return resolve.Result{}, ErrSelectionCancelled
	}
	return picked, nil
}

func writeCandidatesHint(w io.Writer, results []resolve.Result) {
	if w == nil {
		return
	}
	if len(results) > 5 {
		results = results[:5]
	}
	for _, result := range results {
		fmt.Fprintln(w, FormatSeriesCandidate(result.Summary))
	}
	fmt.Fprintf(w, "retry with one of: kura <cmd> <metadataRef>\n")
}

func selectResolveCandidate(io stdio.Stdio, terms []string, results []resolve.Result) (resolve.Result, bool, error) {
	if len(results) == 0 {
		return resolve.Result{}, false, nil
	}
	if len(results) > 5 {
		results = results[:5]
	}
	stdin, ok := io.In.(*os.File)
	if !ok {
		return resolve.Result{}, false, errInteractiveRequires
	}
	stdout, ok := io.Out.(*os.File)
	if !ok {
		return resolve.Result{}, false, errInteractiveRequires
	}

	options := make([]string, 0, len(results)+1)
	for _, result := range results {
		options = append(options, FormatSeriesCandidate(result.Summary))
	}
	noneOption := "None of these"
	options = append(options, noneOption)

	var selected string
	prompt := &survey.Select{
		Message:  fmt.Sprintf("Multiple metadata matches for %v. Select a match:", terms),
		Options:  options,
		PageSize: len(options),
	}
	if err := survey.AskOne(prompt, &selected, survey.WithStdio(stdin, stdout, io.Err)); err != nil {
		return resolve.Result{}, false, err
	}
	if selected == noneOption {
		return resolve.Result{}, false, nil
	}
	for index, option := range options[:len(results)] {
		if selected == option {
			return results[index], true, nil
		}
	}
	return resolve.Result{}, false, nil
}

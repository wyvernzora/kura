package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/AlecAivazis/survey/v2"
	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/resolve"
	"github.com/wyvernzora/kura/internal/ui/stdio"
)

var (
	ErrNoMetadataMatch     = errors.New("ui: no metadata match")
	ErrSelectionRequired   = errors.New("ui: metadata selection required")
	ErrSelectionCancelled  = errors.New("ui: metadata selection cancelled")
	errInteractiveRequires = errors.New("ui: interactive metadata selection requires file stdio")
)

func ResolveSeries(ctx context.Context, terms []string) (metadata.Series, error) {
	src, err := metadata.SourceFrom(ctx)
	if err != nil {
		return metadata.Series{}, err
	}
	resolver, err := resolve.ResolverFrom(ctx)
	if err != nil {
		return metadata.Series{}, err
	}
	io := stdio.From(ctx)

	resolution, err := resolver.Resolve(ctx, resolve.ParseQuery(terms))
	if err != nil {
		return metadata.Series{}, err
	}

	switch len(resolution.Results) {
	case 0:
		return metadata.Series{}, fmt.Errorf("%w for %v", ErrNoMetadataMatch, terms)
	case 1:
		return fetchSeries(ctx, src, resolution.Results[0].Summary.MetadataRef)
	}

	if !io.IsInteractive() {
		writeCandidatesHint(io.Err, terms, resolution.Results)
		return metadata.Series{}, ErrSelectionRequired
	}
	picked, ok, err := selectResolveCandidate(io, terms, resolution.Results)
	if err != nil {
		return metadata.Series{}, err
	}
	if !ok {
		return metadata.Series{}, ErrSelectionCancelled
	}
	return fetchSeries(ctx, src, picked.Summary.MetadataRef)
}

func fetchSeries(ctx context.Context, src metadata.Source, metadataRef string) (metadata.Series, error) {
	ref, err := domain.ParseMetadataRef(metadataRef)
	if err != nil {
		return metadata.Series{}, err
	}
	if ref.Source() != src.Key() {
		return metadata.Series{}, fmt.Errorf("ui: unsupported metadata ref source %q; expected %s:<id>", ref.Source(), src.Key())
	}
	return src.GetSeries(ctx, ref.ID())
}

func writeCandidatesHint(w io.Writer, terms []string, results []resolve.Result) {
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

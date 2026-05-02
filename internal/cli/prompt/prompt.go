// Package prompt holds the interactive disambiguation prompt used by
// cli.WithResolve when a selector matches multiple candidates and stdin
// is a terminal. Splits the survey/AlecAivazis dependency away from
// other render code so non-interactive tools (MCP, scripts) don't pull
// it in.
package prompt

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/response"
)

// ErrNonInteractive is returned when SelectCandidate is asked to prompt
// but the underlying stdio is not a terminal. The caller is expected to
// surface a "selector requires TTY; pass metadata ref directly" error
// to the user.
var ErrNonInteractive = errors.New("prompt: interactive disambiguation requires a terminal")

// ErrCancelled is returned when the user picks "None of these" or
// otherwise dismisses the prompt.
var ErrCancelled = errors.New("prompt: candidate selection cancelled")

// SelectCandidate displays a survey prompt with up to 5 candidates and
// returns the chosen one. terms are echoed in the prompt header so the
// user remembers what they typed.
func SelectCandidate(io stdio.Stdio, terms []string, candidates []response.Candidate) (response.Candidate, error) {
	if len(candidates) == 0 {
		return response.Candidate{}, errors.New("prompt: no candidates to select from")
	}
	if !io.IsInteractive() {
		WriteHint(io.Err, candidates)
		return response.Candidate{}, ErrNonInteractive
	}
	stdin, ok := io.In.(*os.File)
	if !ok {
		return response.Candidate{}, ErrNonInteractive
	}
	stdout, ok := io.Out.(*os.File)
	if !ok {
		return response.Candidate{}, ErrNonInteractive
	}

	display := candidates
	if len(display) > 5 {
		display = display[:5]
	}
	options := make([]string, 0, len(display)+1)
	for _, c := range display {
		options = append(options, FormatCandidate(c))
	}
	noneOption := "None of these"
	options = append(options, noneOption)

	var selected string
	q := &survey.Select{
		Message:  fmt.Sprintf("Multiple metadata matches for %v. Select a match:", terms),
		Options:  options,
		PageSize: len(options),
	}
	if err := survey.AskOne(q, &selected, survey.WithStdio(stdin, stdout, io.Err)); err != nil {
		return response.Candidate{}, err
	}
	if selected == noneOption {
		return response.Candidate{}, ErrCancelled
	}
	for index, option := range options[:len(display)] {
		if selected == option {
			return display[index], nil
		}
	}
	return response.Candidate{}, ErrCancelled
}

// WriteHint echoes the top-5 candidates to w as a one-line-each summary.
// Non-interactive callers use this to show what they could have picked.
func WriteHint(w io.Writer, candidates []response.Candidate) {
	if w == nil {
		return
	}
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}
	for _, c := range candidates {
		fmt.Fprintln(w, FormatCandidate(c))
	}
	fmt.Fprintf(w, "retry with one of: kura <cmd> <metadataRef>\n")
}

// FormatCandidate produces "<title> | <year> | <ref>" with empty fields
// elided. Used by both the interactive prompt and the non-interactive
// hint.
func FormatCandidate(c response.Candidate) string {
	title := c.PreferredTitle
	if title == "" {
		title = c.CanonicalTitle
	}
	parts := []string{title}
	if c.Year > 0 {
		parts = append(parts, strconv.Itoa(c.Year))
	}
	if c.Ref != "" {
		parts = append(parts, c.Ref.String())
	}
	return strings.Join(parts, " | ")
}

package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/wyvernzora/kura/internal/metadata"
)

func Confirm(stdin io.Reader, stderr io.Writer, prompt string) (bool, error) {
	fmt.Fprint(stderr, prompt)
	reader := bufio.NewReader(stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func SelectSeriesCandidate(stdin *os.File, stdout *os.File, stderr io.Writer, dirname string, results []metadata.SearchResult) (metadata.SearchResult, bool, error) {
	if len(results) == 0 {
		return metadata.SearchResult{}, false, nil
	}
	if len(results) > 5 {
		results = results[:5]
	}
	options := make([]string, 0, len(results)+1)
	for _, result := range results {
		options = append(options, FormatSeriesCandidate(result))
	}
	noneOption := "None of these"
	options = append(options, noneOption)

	var selected string
	prompt := &survey.Select{
		Message:  fmt.Sprintf("No exact metadata match for %q. Select a match:", dirname),
		Options:  options,
		PageSize: len(options),
	}
	if err := survey.AskOne(prompt, &selected, survey.WithStdio(stdin, stdout, stderr)); err != nil {
		return metadata.SearchResult{}, false, err
	}
	if selected == noneOption {
		return metadata.SearchResult{}, false, nil
	}
	for index, option := range options[:len(results)] {
		if selected == option {
			return results[index], true, nil
		}
	}
	return metadata.SearchResult{}, false, nil
}

func FormatSeriesCandidate(result metadata.SearchResult) string {
	title := result.PreferredTitle
	if title == "" {
		title = result.CanonicalTitle
	}
	parts := []string{title}
	if result.Year > 0 {
		parts = append(parts, strconv.Itoa(result.Year))
	}
	if result.MetadataRef != "" {
		parts = append(parts, result.MetadataRef)
	}
	return strings.Join(parts, " | ")
}

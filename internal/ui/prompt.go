package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

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

func FormatSeriesCandidate(result metadata.SeriesSummary) string {
	title := result.PreferredTitle.String()
	if title == "" {
		title = result.CanonicalTitle.String()
	}
	parts := []string{title}
	if result.Year > 0 {
		parts = append(parts, strconv.Itoa(result.Year))
	}
	if result.MetadataRef != "" {
		parts = append(parts, result.MetadataRef.String())
	}
	return strings.Join(parts, " | ")
}

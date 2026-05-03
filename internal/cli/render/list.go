// Package render holds human-readable output helpers for each response
// shape. One file per response shape; each file exposes a single
// Render<Shape>(w, result, asJSON) entrypoint.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/ttacon/chalk"
	"github.com/wyvernzora/kura/internal/cli/style"
	"github.com/wyvernzora/kura/internal/response"
)

// List writes the library list response to w. asJSON toggles
// machine-readable JSON output; otherwise renders a styled table.
func List(w io.Writer, result response.ListResult, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result.Rows)
	}
	styled := style.ShouldStyle(w)
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"STATUS", "TITLE", "SEASONS", "EPISODES", "SCANNED", "ROOT"})
	tw.SetStyle(style.BorderlessTableStyle())
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1},
		{Number: 2},
		{Number: 3},
		{Number: 4},
		{Number: 5},
		{Number: 6},
	})
	now := time.Now()
	for _, row := range result.Rows {
		statusText := string(row.Status)
		if row.Staged {
			statusText += "*"
		}
		tw.AppendRow(table.Row{
			renderListStatus(statusText, styled),
			titleCell(row),
			countCell(row.SeasonCount, row.Status),
			countCell(row.EpisodeCount, row.Status),
			scannedCell(row.LastScanned, row.Status, now),
			row.Root,
		})
	}
	return style.WriteStyledTable(w, tw, nil)
}

// titleCell renders the row title for the table. Untracked rows have
// no provider-derived title; mark with `*` to make clear the value
// is inferred from the directory name rather than from metadata.
func titleCell(row response.ListRow) string {
	if row.Status == response.ListStatusUntracked {
		return row.Title + "*"
	}
	return row.Title
}

func countCell(count int, status response.ListStatus) string {
	if status == response.ListStatusUntracked || status == response.ListStatusError {
		return "-"
	}
	return strconv.Itoa(count)
}

func scannedCell(lastScanned string, status response.ListStatus, now time.Time) string {
	if status == response.ListStatusUntracked || status == response.ListStatusError {
		return "-"
	}
	if lastScanned == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, lastScanned)
	if err != nil {
		return "-"
	}
	return relativeAge(now.Sub(t))
}

func relativeAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	}
}

func renderListStatus(status string, styled bool) string {
	value := strings.TrimSpace(status)
	if !styled {
		return value
	}
	base := strings.TrimSuffix(value, "*")
	suffix := strings.TrimPrefix(value, base)
	switch base {
	case string(response.ListStatusUntracked):
		return chalk.Dim.TextStyle(style.Gray(base)) + suffix
	case string(response.ListStatusComplete):
		return chalk.Green.Color(base) + suffix
	case string(response.ListStatusIncomplete):
		return chalk.Bold.TextStyle(chalk.Red.Color(base)) + suffix
	case string(response.ListStatusAiring):
		return chalk.Blue.Color(base) + suffix
	case string(response.ListStatusError):
		return chalk.Bold.TextStyle(chalk.Red.Color(base)) + suffix
	default:
		return value
	}
}

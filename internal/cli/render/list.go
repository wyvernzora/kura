// Package render holds human-readable output helpers for each response
// shape. One file per response shape; each file exposes a single
// Render<Shape>(w, result, asJSON) entrypoint.
package render

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"

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
	tw.AppendHeader(table.Row{"STATUS", "TITLE", "SEASONS", "EPISODES", "ROOT"})
	tw.SetStyle(style.BorderlessTableStyle())
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1},
		{Number: 2},
		{Number: 3},
		{Number: 4},
		{Number: 5},
	})
	for _, row := range result.Rows {
		statusText := string(row.Status)
		if row.Staged {
			statusText += "*"
		}
		tw.AppendRow(table.Row{
			renderListStatus(statusText, styled),
			row.Title,
			countCell(row.SeasonCount, row.Status),
			countCell(row.EpisodeCount, row.Status),
			row.Root,
		})
	}
	return style.WriteStyledTable(w, tw, nil)
}

func countCell(count int, status response.ListStatus) string {
	if status == response.ListStatusUntracked || status == response.ListStatusError {
		return "-"
	}
	return strconv.Itoa(count)
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

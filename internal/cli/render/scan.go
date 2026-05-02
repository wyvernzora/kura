package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/wyvernzora/kura/internal/cli/style"
	"github.com/wyvernzora/kura/internal/response"
)

// Scan writes the scan response. asJSON toggles JSON output; otherwise
// emits a styled per-episode summary table followed by a skipped table
// when relevant.
func Scan(w io.Writer, result response.ScanResult, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	tty := style.ShouldStyle(w)
	if len(result.Synced) == 0 && tty {
		if _, err := fmt.Fprintln(w, "\nNo files found."); err != nil {
			return err
		}
	} else {
		tw := table.NewWriter()
		tw.AppendHeader(table.Row{"EPISODE", "STATUS", "SOURCE", "RESOLUTION", "FILE"})
		tw.SetStyle(style.BorderlessTableStyle())
		tw.SetColumnConfigs([]table.ColumnConfig{
			{Number: 1},
			{Number: 2},
			{Number: 3},
			{Number: 4},
			{Number: 5},
		})
		for _, entry := range result.Synced {
			tw.AppendRow(table.Row{
				entry.Episode.Marker(),
				style.EpisodeStatus(string(entry.Status), tty),
				style.MediaSource(entry.Source, tty),
				style.MediaResolution(entry.Resolution, tty),
				entry.Path,
			})
			for index, companion := range entry.Companions {
				prefix := "    ┣ "
				if index == len(entry.Companions)-1 {
					prefix = "    ┗ "
				}
				tw.AppendRow(table.Row{"", "", "", "", prefix + companion})
			}
		}
		dimLine := func(line string) bool {
			return strings.Contains(line, "┣ ") || strings.Contains(line, "┗ ") || strings.HasPrefix(strings.TrimSpace(line), "unchanged")
		}
		if err := style.WriteStyledTable(w, tw, dimLine); err != nil {
			return err
		}
	}
	if len(result.Skipped) == 0 {
		return nil
	}
	skippedTable := table.NewWriter()
	skippedTable.AppendHeader(table.Row{"SKIPPED FILE", "CODE", "REASON"})
	skippedTable.SetStyle(style.BorderlessTableStyle())
	skippedTable.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1},
		{Number: 2},
		{Number: 3},
	})
	for _, skip := range result.Skipped {
		skippedTable.AppendRow(table.Row{skip.Path, skip.Code, skip.Reason})
	}
	return style.WriteStyledTable(w, skippedTable, nil)
}

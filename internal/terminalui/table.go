package terminalui

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/ttacon/chalk"
	"github.com/wyvernzora/kura/internal/library"
)

func WriteSeriesSyncResult(w io.Writer, result library.SeriesSyncResult) error {
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"STATUS", "SEASON", "EPISODE", "SOURCE", "RESOLUTION", "FILE"})
	tw.SetStyle(borderlessTableStyle())
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1},
		{Number: 2},
		{Number: 3},
		{Number: 4},
		{Number: 5},
		{Number: 6},
	})
	for _, entry := range result.Synced {
		tw.AppendRow(table.Row{
			entry.Status,
			strconv.Itoa(entry.Season),
			strconv.Itoa(entry.Number),
			entry.Source,
			entry.Resolution,
			entry.Path,
		})
		for index, companion := range entry.Companions {
			prefix := "    ┣ "
			if index == len(entry.Companions)-1 {
				prefix = "    ┗ "
			}
			tw.AppendRow(table.Row{"", "", "", "", "", prefix + companion})
		}
	}
	if err := writeStyledTable(w, tw, func(line string) bool {
		return strings.Contains(line, "┣ ") || strings.Contains(line, "┗ ") || strings.HasPrefix(strings.TrimSpace(line), "existing")
	}); err != nil {
		return err
	}
	if len(result.Skipped) == 0 {
		return nil
	}

	skippedTable := table.NewWriter()
	skippedTable.AppendHeader(table.Row{"SKIPPED FILE", "CODE", "REASON"})
	skippedTable.SetStyle(borderlessTableStyle())
	skippedTable.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1},
		{Number: 2},
		{Number: 3},
	})
	for _, skipped := range result.Skipped {
		skippedTable.AppendRow(table.Row{skipped.Path, skipped.Code, skipped.Reason})
	}
	return writeStyledTable(w, skippedTable, nil)
}

func WriteReconcilePlan(w io.Writer, plan library.ReconcilePlan) error {
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"KIND", "FROM", "TO"})
	tw.SetStyle(borderlessTableStyle())
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1},
		{Number: 2},
		{Number: 3},
	})
	for _, move := range plan.FileMoves {
		tw.AppendRow(table.Row{"FILE", move.From, move.To})
	}
	return writeStyledTable(w, tw, nil)
}

func borderlessTableStyle() table.Style {
	style := table.StyleDefault
	style.Options = table.OptionsNoBordersAndSeparators
	return style
}

func writeStyledTable(w io.Writer, tw table.Writer, dimLine func(string) bool) error {
	rendered := tw.Render()
	if rendered == "" {
		return nil
	}
	lines := strings.Split(rendered, "\n")
	if file, ok := w.(*os.File); ok && IsTerminal(file) {
		width := TerminalWidth(file)
		if width > 0 {
			lines[0] = padRight(lines[0], width)
		}
		lines[0] = chalk.Inverse.TextStyle(chalk.Bold.TextStyle(lines[0]))
		for index := 1; index < len(lines); index++ {
			if dimLine != nil && dimLine(lines[index]) {
				lines[index] = chalk.Dim.TextStyle(lines[index])
			}
		}
	}
	_, err := fmt.Fprintf(w, "\n%s\n", strings.Join(lines, "\n"))
	return err
}

func padRight(value string, width int) string {
	if width <= 0 {
		return value
	}
	if len([]rune(value)) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len([]rune(value)))
}

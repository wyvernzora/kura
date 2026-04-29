package ui

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/ttacon/chalk"
	"github.com/wyvernzora/kura/internal/kura"
	"github.com/wyvernzora/kura/internal/ui/stdio"
)

func WriteScanResult(w io.Writer, result kura.ScanResult) error {
	entries := make([]scanTableEntry, 0, len(result.Synced))
	for _, entry := range result.Synced {
		entries = append(entries, scanTableEntry{
			Status:     string(entry.Status),
			Season:     entry.Season,
			Number:     entry.Number,
			Source:     entry.Source,
			Resolution: entry.Resolution,
			Path:       entry.Path,
			Companions: entry.Companions,
		})
	}
	return writeScanTable(w, entries, result.Skipped)
}

type scanTableEntry struct {
	Status     string
	Season     int
	Number     int
	Source     string
	Resolution string
	Path       string
	Companions []string
}

func writeScanTable(w io.Writer, entries []scanTableEntry, skipped []kura.ImportSkip) error {
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
	for _, entry := range entries {
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
	if len(skipped) == 0 {
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
	for _, skipped := range skipped {
		skippedTable.AppendRow(table.Row{skipped.Path, skipped.Code, skipped.Reason})
	}
	return writeStyledTable(w, skippedTable, nil)
}

func WriteKuraReconcilePlan(w io.Writer, plan kura.ReconcilePlan) error {
	var moves []kura.FileMove
	for _, change := range plan.Changes {
		moves = append(moves, change.Moves()...)
	}
	return writeReconcileMoves(w, moves)
}

func writeReconcileMoves(w io.Writer, moves []kura.FileMove) error {
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"KIND", "FROM", "TO"})
	tw.SetStyle(borderlessTableStyle())
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1},
		{Number: 2},
		{Number: 3},
	})
	for _, move := range moves {
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
	if file, ok := w.(*os.File); ok && stdio.IsTerminal(file) {
		width := stdio.TerminalWidth(file)
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

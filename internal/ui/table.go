package ui

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/ttacon/chalk"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series"
	"github.com/wyvernzora/kura/internal/ui/stdio"
)

func WriteSeriesRead(w io.Writer, result series.Series) error {
	if _, err := fmt.Fprintf(w, "MetadataRef: %s\n", result.MetadataRef); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Root: %s\n", result.Root); err != nil {
		return err
	}
	title := result.PreferredTitle.String()
	if result.CanonicalTitle != nil && !result.CanonicalTitle.IsZero() && *result.CanonicalTitle != result.PreferredTitle {
		title += " / " + result.CanonicalTitle.String()
	}
	if _, err := fmt.Fprintf(w, "Title: %s\n", title); err != nil {
		return err
	}
	for _, season := range result.Seasons {
		label := "SEASON " + strconv.Itoa(season.Number)
		if season.Number == 0 {
			label = "SPECIALS"
		}
		if _, err := fmt.Fprintf(w, "\n%s\n", label); err != nil {
			return err
		}
		if err := writeEpisodeReadTable(w, season.Episodes); err != nil {
			return err
		}
	}
	return nil
}

func writeEpisodeReadTable(w io.Writer, episodes []series.Episode) error {
	style := shouldStyle(w)
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"NUMBER", "STATUS", "SOURCE", "RESOLUTION", "FILE"})
	tw.SetStyle(borderlessTableStyle())
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1},
		{Number: 2},
		{Number: 3},
		{Number: 4},
		{Number: 5},
	})
	for _, episode := range episodes {
		if episode.Active != nil && episode.Staged != nil {
			tw.AppendRow(readEpisodeRow(episode.Episode.Episode(), series.EpisodeStatusPresent, episode.Active, style, true))
			tw.AppendRow(readEpisodeRow(episode.Episode.Episode(), series.EpisodeStatusStaged, episode.Staged, style, false))
			continue
		}
		tw.AppendRow(readEpisodeRow(episode.Episode.Episode(), episode.Status, firstEpisodeMedia(episode), style, false))
	}
	return writeStyledTable(w, tw, nil)
}

func readEpisodeRow(number int, status series.EpisodeStatus, media *series.EpisodeMedia, style bool, retired bool) table.Row {
	statusCell := string(status)
	source := ""
	resolution := ""
	file := ""
	if media != nil {
		source = media.Source
		resolution = media.Resolution
		file = media.File
	}
	row := table.Row{
		strconv.Itoa(number),
		statusCell,
		source,
		resolution,
		file,
	}
	if !style {
		return row
	}
	if retired {
		for index := range row {
			row[index] = retireCell(row[index].(string))
		}
		return row
	}
	row[1] = styleEpisodeStatus(status, true)
	row[2] = styleMediaSource(source, true)
	row[3] = styleMediaResolution(resolution, true)
	return row
}

func firstEpisodeMedia(episode series.Episode) *series.EpisodeMedia {
	if episode.Staged != nil {
		return episode.Staged
	}
	return episode.Active
}

func retireCell(value string) string {
	if value == "" {
		return ""
	}
	return chalk.Dim.TextStyle(chalk.Strikethrough.TextStyle(value))
}

func styleEpisodeStatus(status series.EpisodeStatus, style bool) string {
	value := string(status)
	if !style {
		return value
	}
	switch status {
	case series.EpisodeStatusMissing:
		return orange(value)
	case series.EpisodeStatusUnavailable:
		return chalk.Bold.TextStyle(chalk.Red.Color(value))
	case series.EpisodeStatusPresent:
		return chalk.Green.Color(value)
	case series.EpisodeStatusPending:
		return chalk.Dim.TextStyle(gray(value))
	case series.EpisodeStatusStaged:
		return chalk.Yellow.Color(value)
	default:
		return value
	}
}

func styleMediaSource(source string, style bool) string {
	if !style {
		return source
	}
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "bdrip", "bluray", "blu-ray":
		return chalk.Green.Color(source)
	case "web-dl", "webdl", "web-rip", "webrip":
		return chalk.Yellow.Color(source)
	case "tv", "hdtv", "tvrip", "tv-rip":
		return orange(source)
	case "unknown":
		return chalk.Red.Color(source)
	default:
		return source
	}
}

func styleMediaResolution(resolution string, style bool) string {
	if !style {
		return resolution
	}
	switch strings.ToLower(strings.TrimSpace(resolution)) {
	case "4k":
		return chalk.Blue.Color(resolution)
	case "1080p":
		return chalk.Green.Color(resolution)
	case "720p":
		return chalk.Red.Color(resolution)
	case "":
		return resolution
	default:
		return orange(resolution)
	}
}

func orange(value string) string {
	return "\x1b[38;5;208m" + value + "\x1b[39m"
}

func gray(value string) string {
	return "\x1b[90m" + value + "\x1b[39m"
}

func WriteScanResult(w io.Writer, result series.ScanResult) error {
	entries := make([]scanTableEntry, 0, len(result.Synced))
	for _, entry := range result.Synced {
		entries = append(entries, scanTableEntry{
			Status:     string(entry.Status),
			Episode:    entry.Episode,
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
	Episode    refs.Episode
	Source     string
	Resolution string
	Path       string
	Companions []string
}

func writeScanTable(w io.Writer, entries []scanTableEntry, skipped []series.ImportSkip) error {
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
			strconv.Itoa(entry.Episode.Season()),
			strconv.Itoa(entry.Episode.Episode()),
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

func WriteReconcilePlan(w io.Writer, plan series.ReconcilePlan) error {
	var moves []series.FileMove
	for _, change := range plan.Changes {
		moves = append(moves, change.Moves()...)
	}
	return writeReconcileMoves(w, moves)
}

func writeReconcileMoves(w io.Writer, moves []series.FileMove) error {
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
	if shouldStyle(w) {
		file := w.(*os.File)
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

func shouldStyle(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && stdio.IsTerminal(file)
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

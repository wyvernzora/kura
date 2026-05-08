package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/ttacon/chalk"
	"github.com/wyvernzora/kura/internal/cli/style"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/response"
)

// Show writes the show response to w. asJSON toggles machine-readable
// JSON output; otherwise emits the human-readable per-season tables.
func Show(w io.Writer, result response.Show, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	styled := style.ShouldStyle(w)
	if err := writeShowHeader(w, result, styled); err != nil {
		return err
	}
	if err := writeShowSeasons(w, result); err != nil {
		return err
	}
	return writeShowStaged(w, result)
}

// writeShowHeader emits the per-series header rows (ID / Title / Root
// always; Status + LastScanned when populated). Title combines
// preferred + canonical when they diverge.
func writeShowHeader(w io.Writer, result response.Show, styled bool) error {
	title := result.PreferredTitle
	if result.CanonicalTitle != "" && result.CanonicalTitle != result.PreferredTitle {
		title += " / " + result.CanonicalTitle
	}
	rows := []struct{ label, value string }{
		{"ID", styleShowValue(result.MetadataRef.String(), styled)},
		{"Title", styleShowValue(title, styled)},
		{"Root", styleShowValue(result.Root, styled)},
	}
	if result.Status != "" {
		rows = append(rows, struct{ label, value string }{
			"Status", renderListStatus(string(result.Status), styled),
		})
	}
	if result.LastScanned != "" {
		rows = append(rows, struct{ label, value string }{
			"LastScanned", styleShowValue(result.LastScanned, styled),
		})
	}
	for _, row := range rows {
		label := row.label
		if styled {
			label = chalk.Bold.TextStyle(chalk.White.Color(label))
		}
		if _, err := fmt.Fprintf(w, "%s: %s\n", label, row.value); err != nil {
			return err
		}
	}
	return nil
}

// writeShowSeasons emits one section per season, with season 0
// rendered as "SPECIALS" instead of "SEASON 0".
func writeShowSeasons(w io.Writer, result response.Show) error {
	for _, season := range result.Seasons {
		label := "SEASON " + strconv.Itoa(season.Number)
		if season.Number == 0 {
			label = "SPECIALS"
		}
		if _, err := fmt.Fprintf(w, "\n%s\n", label); err != nil {
			return err
		}
		if err := writeShowSeasonTable(w, result.Root, season.Episodes); err != nil {
			return err
		}
	}
	return nil
}

// writeShowStaged emits the STAGED TRASH and STAGED EXTRAS sections
// when their respective slices are non-empty. Empty sections are
// skipped silently.
func writeShowStaged(w io.Writer, result response.Show) error {
	if len(result.StagedTrash) > 0 {
		if _, err := fmt.Fprint(w, "\nSTAGED TRASH\n"); err != nil {
			return err
		}
		if err := writeStagedTrashTable(w, result.StagedTrash); err != nil {
			return err
		}
	}
	if len(result.StagedExtras) > 0 {
		if _, err := fmt.Fprint(w, "\nSTAGED EXTRAS\n"); err != nil {
			return err
		}
		if err := writeStagedExtrasTable(w, result.StagedExtras); err != nil {
			return err
		}
	}
	return nil
}

func styleShowValue(value string, styled bool) string {
	if !styled || value == "" {
		return value
	}
	return chalk.Yellow.Color(value)
}

func writeStagedTrashTable(w io.Writer, items []response.TrashItemShow) error {
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"ID", "PATH", "SIZE", "COMPANIONS"})
	tw.SetStyle(style.BorderlessTableStyle())
	for _, item := range items {
		companions := ""
		if n := len(item.Companions); n == 1 {
			companions = "+1 companion"
		} else if n > 1 {
			companions = fmt.Sprintf("+%d companions", n)
		}
		tw.AppendRow(table.Row{item.ID, item.Path, formatSkipSize(item.Size), companions})
	}
	return style.WriteStyledTable(w, tw, nil)
}

func writeStagedExtrasTable(w io.Writer, items []response.ExtraItemShow) error {
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"ID", "SEASON", "KIND", "PREFIX", "PATH"})
	tw.SetStyle(style.BorderlessTableStyle())
	for _, item := range items {
		kind := "file"
		if item.IsDir {
			kind = "dir"
		}
		tw.AppendRow(table.Row{item.ID, "S" + strconv.Itoa(item.Season), kind, item.Prefix, item.Path})
	}
	return style.WriteStyledTable(w, tw, nil)
}

func writeShowSeasonTable(w io.Writer, seriesRoot string, episodes []response.EpisodeShow) error {
	styled := style.ShouldStyle(w)
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"EPISODE", "AIRED", "STATUS", "SOURCE", "RESOLUTION", "TITLE", "FILE"})
	tw.SetStyle(style.BorderlessTableStyle())
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1},
		{Number: 2},
		{Number: 3},
		{Number: 4},
		{Number: 5},
		{Number: 6},
		{Number: 7},
	})
	for _, episode := range episodes {
		if episode.Active != nil && episode.Staged != nil {
			appendShowRows(tw, seriesRoot, episode.Episode, episode.Aired, episode.PreferredTitle, response.StatusPresent, episode.Active, styled, true)
			appendShowRows(tw, seriesRoot, episode.Episode, episode.Aired, episode.PreferredTitle, response.StatusStaged, episode.Staged, styled, false)
			continue
		}
		appendShowRows(tw, seriesRoot, episode.Episode, episode.Aired, episode.PreferredTitle, episode.Status, firstShowMedia(episode), styled, false)
	}
	return style.WriteStyledTable(w, tw, isCompanionLine)
}

// isCompanionLine matches the rendered companion-row prefix used by
// appendShowRows so WriteStyledTable can dim sidecar entries.
func isCompanionLine(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	return strings.HasPrefix(trimmed, "┣ ") || strings.HasPrefix(trimmed, "┗ ")
}

func appendShowRows(tw table.Writer, seriesRoot string, episode refs.Episode, aired, title string, status response.Status, media *response.MediaShow, styled bool, retired bool) {
	tw.AppendRow(showEpisodeRow(seriesRoot, episode, aired, title, status, media, styled, retired))
	if media == nil {
		return
	}
	for index, companion := range media.Companions {
		prefix := "    ┣ "
		if index == len(media.Companions)-1 {
			prefix = "    ┗ "
		}
		row := table.Row{"", "", "", "", "", "", prefix + showRelPath(seriesRoot, companion.Path)}
		if styled && retired {
			for index := range row {
				row[index] = style.Retired(row[index].(string))
			}
		}
		tw.AppendRow(row)
	}
}

func showEpisodeRow(seriesRoot string, episode refs.Episode, aired, title string, status response.Status, media *response.MediaShow, styled bool, retired bool) table.Row {
	source := ""
	resolution := ""
	file := ""
	if media != nil {
		source = displaySource(media.Source, status)
		resolution = media.Resolution
		file = showRelPath(seriesRoot, media.File)
	}
	row := table.Row{
		episode.Marker(),
		aired,
		string(status),
		source,
		resolution,
		title,
		file,
	}
	if !styled {
		return row
	}
	if retired {
		for index := range row {
			row[index] = style.Retired(row[index].(string))
		}
		return row
	}
	row[2] = style.EpisodeStatus(string(status), true)
	row[3] = style.MediaSource(source, true)
	row[4] = style.MediaResolution(resolution, true)
	return row
}

// showRelPath strips the series root prefix from path so the table column
// stays narrow. Paths outside the series dir (typically staged inbox files)
// are returned as-is.
func showRelPath(seriesRoot, path string) string {
	if path == "" || seriesRoot == "" {
		return path
	}
	prefix := seriesRoot
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if strings.HasPrefix(path, prefix) {
		return path[len(prefix):]
	}
	return path
}

// displaySource hides the placeholder "Unknown" source for episodes that
// have no recorded media. Recorded sources are still shown for present +
// staged rows even when they happen to be Unknown.
func displaySource(source string, status response.Status) string {
	if source == "Unknown" && (status == response.StatusMissing || status == response.StatusPending) {
		return ""
	}
	return source
}

func firstShowMedia(episode response.EpisodeShow) *response.MediaShow {
	if episode.Staged != nil {
		return episode.Staged
	}
	return episode.Active
}

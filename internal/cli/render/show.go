package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
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
	if _, err := fmt.Fprintf(w, "MetadataRef: %s\n", result.MetadataRef); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Root: %s\n", result.Root); err != nil {
		return err
	}
	if result.LastScanned != "" {
		if _, err := fmt.Fprintf(w, "LastScanned: %s\n", result.LastScanned); err != nil {
			return err
		}
	}
	title := result.PreferredTitle
	if result.CanonicalTitle != "" && result.CanonicalTitle != result.PreferredTitle {
		title += " / " + result.CanonicalTitle
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
		if err := writeShowSeasonTable(w, result.Root, season.Episodes); err != nil {
			return err
		}
	}
	return nil
}

func writeShowSeasonTable(w io.Writer, seriesRoot string, episodes []response.EpisodeShow) error {
	styled := style.ShouldStyle(w)
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
	for _, episode := range episodes {
		if episode.Active != nil && episode.Staged != nil {
			appendShowRows(tw, seriesRoot, episode.Episode, response.StatusPresent, episode.Active, styled, true)
			appendShowRows(tw, seriesRoot, episode.Episode, response.StatusStaged, episode.Staged, styled, false)
			continue
		}
		appendShowRows(tw, seriesRoot, episode.Episode, episode.Status, firstShowMedia(episode), styled, false)
	}
	return style.WriteStyledTable(w, tw, nil)
}

func appendShowRows(tw table.Writer, seriesRoot string, episode refs.Episode, status response.Status, media *response.MediaShow, styled bool, retired bool) {
	tw.AppendRow(showEpisodeRow(seriesRoot, episode, status, media, styled, retired))
	if media == nil {
		return
	}
	for index, companion := range media.Companions {
		prefix := "    ┣ "
		if index == len(media.Companions)-1 {
			prefix = "    ┗ "
		}
		row := table.Row{"", "", "", "", prefix + showRelPath(seriesRoot, companion.Path)}
		if styled && retired {
			for index := range row {
				row[index] = style.Retired(row[index].(string))
			}
		}
		tw.AppendRow(row)
	}
}

func showEpisodeRow(seriesRoot string, episode refs.Episode, status response.Status, media *response.MediaShow, styled bool, retired bool) table.Row {
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
		string(status),
		source,
		resolution,
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
	row[1] = style.EpisodeStatus(string(status), true)
	row[2] = style.MediaSource(source, true)
	row[3] = style.MediaResolution(resolution, true)
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

// displaySource hides the placeholder "Unknown" source for episodes whose
// file is not actually available on disk. Recorded sources are still shown
// for present + staged rows even when they happen to be Unknown.
func displaySource(source string, status response.Status) string {
	if source == "Unknown" && (status == response.StatusUnavailable || status == response.StatusMissing || status == response.StatusPending) {
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

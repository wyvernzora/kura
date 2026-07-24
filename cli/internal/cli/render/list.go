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
	"github.com/wyvernzora/kura/cli/internal/cli/style"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

// List writes the library list response to w. asJSON toggles
// machine-readable JSON output; otherwise renders a styled table.
func List(w io.Writer, result api.ListResult, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result.Rows)
	}
	styled := style.ShouldStyle(w)
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"STATUS", "ID", "SEASONS", "EPISODES", "RESOLUTION", "SOURCE", "TITLE", "SCANNED"})
	tw.SetStyle(style.BorderlessTableStyle())
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1},
		{Number: 2},
		{Number: 3},
		{Number: 4},
		{Number: 5},
		{Number: 6},
		{Number: 7},
		{Number: 8},
	})
	now := time.Now()
	for _, row := range result.Rows {
		statusText := string(row.Status)
		if row.Staged {
			statusText += "*"
		}
		tw.AppendRow(table.Row{
			renderListStatus(statusText, row.IsAiring, styled),
			idCell(row),
			progressCell(row.SeasonsAvailable, row.SeasonCount, row.Status, styled),
			progressCell(row.EpisodesAvailable, row.EpisodeCount, row.Status, styled),
			resolutionListCell(row.Resolutions, styled),
			sourceListCell(row.Sources, styled),
			titleCell(row),
			scannedCell(row.LastScanned, row.Status, now),
		})
	}
	return style.WriteStyledTable(w, tw, nil)
}

// idCell renders the metadata ref ("ID" column). Single dash for
// untracked / error rows that have no metadata ref.
func idCell(row api.ListRow) string {
	if row.MetadataRef == "" {
		return "-"
	}
	return row.MetadataRef.String()
}

// resolutionListCell renders the per-series distinct-resolutions
// roll-up. Each value styled via the shared MediaResolution helper
// so colors match the show / scan tables.
func resolutionListCell(values []string, styled bool) string {
	if len(values) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, style.MediaResolution(v, styled))
	}
	return strings.Join(parts, " ")
}

// sourceListCell renders the per-series distinct-sources roll-up.
// Each value styled via the shared MediaSource helper.
func sourceListCell(values []string, styled bool) string {
	if len(values) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, style.MediaSource(v, styled))
	}
	return strings.Join(parts, " ")
}

// titleCell renders the row title for the table. Untracked rows have
// no provider-derived title; mark with `*` to make clear the value
// is inferred from the directory name rather than from metadata.
func titleCell(row api.ListRow) string {
	if row.Status == api.ListStatusUntracked {
		return row.Title + "*"
	}
	return row.Title
}

// progressCell renders an "available/total" pair (e.g. "10/12").
// Untracked / error rows return "-" since the model is unknown.
// When styled, equal counts render green; mismatches render red so
// incomplete series are visible at a glance. Specials are excluded
// from both numerator and denominator at the workflow layer.
func progressCell(available, total int, status api.ListStatus, styled bool) string {
	if status == api.ListStatusUntracked || status == api.ListStatusError {
		return "-"
	}
	text := strconv.Itoa(available) + "/" + strconv.Itoa(total)
	if !styled {
		return text
	}
	if available == total {
		return style.Green(text)
	}
	return style.Red(text)
}

func scannedCell(lastScanned string, status api.ListStatus, now time.Time) string {
	if status == api.ListStatusUntracked || status == api.ListStatusError {
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

// airingBadge is the white-on-blue " airing " chip appended to the
// status cell when row.IsAiring is true. One space pad each side.
var airingBadge = style.WhiteOnBlue(" airing ")

func renderListStatus(status string, isAiring, styled bool) string {
	value := strings.TrimSpace(status)
	if !styled {
		if isAiring {
			return value + " (airing)"
		}
		return value
	}
	base := strings.TrimSuffix(value, "*")
	suffix := strings.TrimPrefix(value, base)
	var rendered string
	switch base {
	case string(api.ListStatusUntracked):
		rendered = style.Dim(style.Gray(base)) + suffix
	case string(api.ListStatusComplete):
		rendered = style.Green(base) + suffix
	case string(api.ListStatusIncomplete):
		rendered = style.Bold(style.Red(base)) + suffix
	case string(api.ListStatusError):
		rendered = style.Bold(style.Red(base)) + suffix
	default:
		rendered = value
	}
	if isAiring {
		rendered += " " + airingBadge
	}
	return rendered
}

package render

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/wyvernzora/kura/internal/cli/style"
	"github.com/wyvernzora/kura/internal/response"
)

// TrashList writes the trash-list response. asJSON forces JSON output
// even on a TTY; non-TTY callers always get JSON. Otherwise renders one
// styled table per series with a summary header.
func TrashList(w io.Writer, result response.TrashList, asJSON bool) error {
	if asJSON || !style.ShouldStyle(w) {
		return writeJSON(w, result)
	}
	if len(result.Series) == 0 {
		_, err := fmt.Fprintln(w, "\nNo trash entries.")
		return err
	}
	for _, series := range result.Series {
		if _, err := fmt.Fprintf(w, "\n==== %s (%s, %s) ====\n",
			series.Ref, entriesLabel(len(series.Entries)), formatBytes(series.Bytes)); err != nil {
			return err
		}
		tw := table.NewWriter()
		tw.AppendHeader(table.Row{"ULID", "EPISODE", "TRASHED", "MEDIA", "SIZE"})
		tw.SetStyle(style.BorderlessTableStyle())
		tw.SetColumnConfigs([]table.ColumnConfig{
			{Number: 1}, {Number: 2}, {Number: 3}, {Number: 4}, {Number: 5},
		})
		for _, entry := range series.Entries {
			tw.AppendRow(table.Row{
				entry.ID,
				entry.Episode.Marker(),
				entry.TrashedAt.Format("2006-01-02 15:04Z"),
				entry.MediaPath,
				formatBytes(entry.Size),
			})
		}
		if err := style.WriteStyledTable(w, tw, nil); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\nTotal: %s, %s across %d series\n",
		entriesLabel(result.TotalEntries), formatBytes(result.TotalBytes), len(result.Series))
	return err
}

// TrashEmpty writes the trash-empty response.
func TrashEmpty(w io.Writer, result response.TrashEmpty, asJSON bool) error {
	if asJSON || !style.ShouldStyle(w) {
		return writeJSON(w, result)
	}
	if result.TotalEntries == 0 {
		_, err := fmt.Fprintln(w, "Nothing to empty.")
		return err
	}
	_, err := fmt.Fprintf(w, "Removed %s, reclaimed %s across %d series.\n",
		entriesLabel(result.TotalEntries), formatBytes(result.ReclaimedBytes), len(result.Series))
	return err
}

// TrashRestore writes the trash-restore response.
func TrashRestore(w io.Writer, result response.TrashRestore, asJSON bool) error {
	if asJSON || !style.ShouldStyle(w) {
		return writeJSON(w, result)
	}
	_, err := fmt.Fprintf(w, "Restored %d files for %s %s.\n",
		len(result.Restored), result.Ref, result.Episode.Marker())
	return err
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func entriesLabel(n int) string {
	if n == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", n)
}

func formatBytes(bytes int64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)
	switch {
	case bytes >= gib:
		return fmt.Sprintf("%.1f GiB", float64(bytes)/float64(gib))
	case bytes >= mib:
		return fmt.Sprintf("%.1f MiB", float64(bytes)/float64(mib))
	case bytes >= kib:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/float64(kib))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

package render

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/wyvernzora/kura/internal/cli/style"
	"github.com/wyvernzora/kura/internal/response"
)

// Resolve writes the resolution response. asJSON toggles JSON output;
// otherwise emits a styled candidate table. Empty results print a one-line
// "no matches" note when on a TTY.
func Resolve(w io.Writer, result response.Resolution, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	tty := style.ShouldStyle(w)
	if len(result.Candidates) == 0 {
		if tty {
			if _, err := fmt.Fprintln(w, "\nNo matches."); err != nil {
				return err
			}
		}
		return nil
	}
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"REF", "TITLE", "YEAR", "LANG", "FIRST AIRED"})
	tw.SetStyle(style.BorderlessTableStyle())
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1},
		{Number: 2},
		{Number: 3},
		{Number: 4},
		{Number: 5},
	})
	for _, candidate := range result.Candidates {
		title := candidate.PreferredTitle
		if candidate.CanonicalTitle != "" && candidate.CanonicalTitle != candidate.PreferredTitle {
			title += " / " + candidate.CanonicalTitle
		}
		year := ""
		if candidate.Year > 0 {
			year = fmt.Sprintf("%d", candidate.Year)
		}
		tw.AppendRow(table.Row{
			candidate.Ref.String(),
			title,
			year,
			candidate.OriginalLanguage,
			candidate.FirstAired,
		})
	}
	return style.WriteStyledTable(w, tw, nil)
}

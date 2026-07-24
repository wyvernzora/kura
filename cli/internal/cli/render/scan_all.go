package render

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/wyvernzora/kura/cli/internal/cli/style"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

// ScanAll writes the library-wide scan summary. asJSON toggles JSON
// output; otherwise emits a one-line tally followed by a per-failure
// table when relevant.
func ScanAll(w io.Writer, result api.ScanAllResult, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	if _, err := fmt.Fprintf(w, "scan-all: %d ok, %d failed (of %d total)\n",
		result.Succeeded, result.Failed, result.Total); err != nil {
		return err
	}
	if len(result.Failures) == 0 {
		return nil
	}
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"SERIES", "KIND", "MESSAGE"})
	tw.SetStyle(style.BorderlessTableStyle())
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1},
		{Number: 2},
		{Number: 3},
	})
	for _, f := range result.Failures {
		tw.AppendRow(table.Row{f.Ref, f.Kind, f.Message})
	}
	return style.WriteStyledTable(w, tw, nil)
}

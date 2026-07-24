package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/wyvernzora/kura/cli/internal/cli/style"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

// InboxList writes the inbox listing to w. asJSON toggles a JSON
// dump; otherwise renders a styled table.
func InboxList(w io.Writer, result api.InboxList, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	styled := style.ShouldStyle(w)
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"KIND", "SIZE", "MODIFIED", "PATH"})
	tw.SetStyle(style.BorderlessTableStyle())
	now := time.Now()
	for _, e := range result.Entries {
		path := stripPathScheme(e.Path)
		switch e.Kind {
		case "dir":
			if !strings.HasSuffix(path, "/") {
				path += "/"
			}
		case "symlink":
			if e.SymlinkTarget != "" {
				path += " -> " + e.SymlinkTarget
			}
		}
		tw.AppendRow(table.Row{
			renderInboxKind(e.Kind, styled),
			renderInboxSize(e.Size, e.Kind),
			renderInboxMTime(e.MTime, now),
			path,
		})
	}
	if err := style.WriteStyledTable(w, tw, nil); err != nil {
		return err
	}
	if result.Truncated {
		fmt.Fprintf(w, "\n... %d entries shown, %d elided.\n", len(result.Entries), result.ElidedCount)
		if len(result.Hint) > 0 {
			fmt.Fprintln(w, "Narrow scope to retry:")
			for _, h := range result.Hint {
				fmt.Fprintf(w, "  %s\n", h)
			}
		}
	}
	return nil
}

// renderInboxMTime formats wire-shape ISO timestamps as relative ages
// ("just now", "5m", "3h ago", "2d ago"). Reuses the same buckets as
// the list renderer's scannedCell. Returns "-" when the input doesn't
// parse so a malformed mtime doesn't break the table layout.
func renderInboxMTime(s string, now time.Time) string {
	if s == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "-"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	}
}

func renderInboxKind(kind string, styled bool) string {
	if !styled {
		return kind
	}
	switch kind {
	case "file":
		return style.Green(kind)
	case "dir":
		return style.Blue(kind)
	case "symlink":
		return style.Yellow(kind)
	default:
		return kind
	}
}

func renderInboxSize(n int64, kind string) string {
	if kind != "file" {
		return "-"
	}
	return humanSize(n)
}

// humanSize is a private CLI variant of the byte-size formatter. The
// MCP renderer has its own copy because the two surfaces are
// architecturally separate (see internal/server/mcp/render).
func humanSize(n int64) string {
	const (
		_  = iota
		kb = 1 << (10 * iota)
		mb
		gb
		tb
	)
	switch {
	case n >= tb:
		return fmt.Sprintf("%.2fTB", float64(n)/float64(tb))
	case n >= gb:
		return fmt.Sprintf("%.2fGB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1fMB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1fKB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

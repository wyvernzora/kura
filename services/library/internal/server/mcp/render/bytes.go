// Package render hosts MCP-side plain-text formatters. Output is
// optimized for agent consumption: terse, monospace-friendly, no
// JSON envelope. Distinct from internal/cli/render — that package
// targets human terminals with color and table styling.
package render

import "fmt"

// HumanByteSize formats a byte count as a short human string. Returns
// "-" for negative values; "1.15GB" / "82.4MB" / "512B" otherwise.
func HumanByteSize(n int64) string {
	if n < 0 {
		return "-"
	}
	const (
		_  = iota
		kb = 1 << (10 * iota)
		mb
		gb
		tb
		pb
	)
	switch {
	case n >= pb:
		return fmt.Sprintf("%.2fPB", float64(n)/float64(pb))
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

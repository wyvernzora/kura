// Package style provides the shared ANSI-styling and table primitives
// used by every cli/render/* file. Keeps render code from each
// re-implementing terminal-detection and color helpers.
package style

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/wyvernzora/kura/cli/internal/cli/stdio"
)

// ShouldStyle returns true when w is a terminal that supports ANSI
// escapes. Render code passes this through to per-cell color helpers.
func ShouldStyle(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && stdio.IsTerminal(file)
}

// BorderlessTableStyle is the default table style across the CLI: no
// borders, no separators, plain text rows.
func BorderlessTableStyle() table.Style {
	style := table.StyleDefault
	style.Options = table.OptionsNoBordersAndSeparators
	return style
}

// WriteStyledTable renders a go-pretty table with the project's
// header-inverse + optional dim-row treatment. dimLine, if non-nil,
// dims rows it returns true for.
func WriteStyledTable(w io.Writer, tw table.Writer, dimLine func(string) bool) error {
	rendered := tw.Render()
	if rendered == "" {
		return nil
	}
	lines := strings.Split(rendered, "\n")
	if ShouldStyle(w) {
		file := w.(*os.File)
		width := stdio.TerminalWidth(file)
		if width > 0 {
			lines[0] = PadRight(lines[0], width)
		}
		lines[0] = Inverse(Bold(lines[0]))
		for index := 1; index < len(lines); index++ {
			if dimLine != nil && dimLine(lines[index]) {
				lines[index] = Dim(lines[index])
			}
		}
	}
	_, err := fmt.Fprintf(w, "\n%s\n", strings.Join(lines, "\n"))
	return err
}

// PadRight pads value with spaces on the right until it reaches width
// runes. Returns value unchanged if it already meets or exceeds width.
func PadRight(value string, width int) string {
	if width <= 0 {
		return value
	}
	if len([]rune(value)) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len([]rune(value)))
}

// Orange is a terminal-orange (256-color 208) wrapper. Used for "warning
// but not error" highlighting (e.g. unknown source).
func Orange(value string) string {
	return "\x1b[38;5;208m" + value + "\x1b[39m"
}

// Gray is a dim-gray wrapper used for de-emphasized text.
func Gray(value string) string {
	return "\x1b[90m" + value + "\x1b[39m"
}

func Green(value string) string { return "\x1b[32m" + value + "\x1b[39m" }

func Yellow(value string) string { return "\x1b[33m" + value + "\x1b[39m" }

func Blue(value string) string { return "\x1b[34m" + value + "\x1b[39m" }

func Red(value string) string { return "\x1b[31m" + value + "\x1b[39m" }

func White(value string) string { return "\x1b[37m" + value + "\x1b[39m" }

func Bold(value string) string { return "\x1b[1m" + value + "\x1b[22m" }

func Dim(value string) string { return "\x1b[2m" + value + "\x1b[22m" }

func Strikethrough(value string) string { return "\x1b[9m" + value + "\x1b[29m" }

func Inverse(value string) string { return "\x1b[7m" + value + "\x1b[27m" }

func WhiteOnBlue(value string) string { return "\x1b[37;44m" + value + "\x1b[39;49m" }

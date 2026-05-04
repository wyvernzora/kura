// Package stdio carries the process IO streams plus terminal facts on a
// context.Context, so commands can resolve interactivity once at startup
// and helpers deep in the call stack can read it without taking it as a
// parameter.
package stdio

import (
	"context"
	"io"
	"os"

	"golang.org/x/term"
)

type contextKey struct{}

// Stdio bundles the process streams together with whether stdin/stdout
// are connected to a terminal. Computed once via New and propagated via
// With/From.
type Stdio struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer

	InIsTTY  bool
	OutIsTTY bool
	ErrIsTTY bool
}

// New builds a Stdio from the given streams, detecting terminal-ness for
// each of stdin, stdout, and stderr.
func New(in io.Reader, out io.Writer, err io.Writer) Stdio {
	return Stdio{
		In:       in,
		Out:      out,
		Err:      err,
		InIsTTY:  isTerminalReader(in),
		OutIsTTY: isTerminalWriter(out),
		ErrIsTTY: isTerminalWriter(err),
	}
}

// With attaches s to ctx. From returns it.
func With(ctx context.Context, s Stdio) context.Context {
	return context.WithValue(ctx, contextKey{}, s)
}

// From returns the Stdio attached to ctx, or the zero value if none was set.
// The zero value reports non-interactive and has nil streams; callers that
// only need IsInteractive can rely on that without checking presence.
func From(ctx context.Context) Stdio {
	if ctx == nil {
		return Stdio{}
	}
	s, _ := ctx.Value(contextKey{}).(Stdio)
	return s
}

// IsInteractive reports whether the user can be prompted: stdin must
// be a TTY (so they can type) and at least one of stdout/stderr must
// be a TTY (so the prompt is visible). Pipelines like
// `kura import --json | jq` capture stdout but leave stderr on the
// terminal; callers route the prompt to stderr in that case.
func (s Stdio) IsInteractive() bool {
	return s.InIsTTY && (s.OutIsTTY || s.ErrIsTTY)
}

// PromptWriter returns the writer the disambiguation prompt should
// render to: prefer stdout when it is a TTY (prompt + result feel
// co-located), otherwise stderr (so a piped --json stdout stays
// machine-readable). Returns nil if neither is a TTY.
func (s Stdio) PromptWriter() io.Writer {
	if s.OutIsTTY {
		return s.Out
	}
	if s.ErrIsTTY {
		return s.Err
	}
	return nil
}

// IsTerminal reports whether file is a character-device terminal.
func IsTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

// TerminalWidth returns the column width of file's terminal, or 0 if it
// is not a terminal or the size cannot be determined.
func TerminalWidth(file *os.File) int {
	if !IsTerminal(file) {
		return 0
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil {
		return 0
	}
	return width
}

func isTerminalReader(r io.Reader) bool {
	file, ok := r.(*os.File)
	return ok && IsTerminal(file)
}

func isTerminalWriter(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && IsTerminal(file)
}

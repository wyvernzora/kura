package main

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
)

// newServerLogger builds the slog handler used by `kura serve`. JSON
// to the supplied writer when not a TTY (k8s container logs, file
// redirects, systemd journal); tinted human format with relative
// timestamps when stderr is an interactive terminal.
//
// Level is supplied by the validated serve config. Unknown values
// still fall back to info so this helper remains safe in unit tests.
func newServerLogger(w io.Writer, rawLevel string) *slog.Logger {
	level := parseLogLevel(rawLevel)
	if file, ok := w.(*os.File); ok && isatty.IsTerminal(file.Fd()) {
		return slog.New(tint.NewHandler(w, &tint.Options{
			Level:      level,
			TimeFormat: time.Kitchen,
		}))
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	}))
}

func parseLogLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

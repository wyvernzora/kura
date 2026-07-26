// Command kura-tape-backup provides the tape archive control plane and
// ephemeral executor entrypoints.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/wyvernzora/kura/services/tape-backup/internal/config"
)

// version and commit are overridable at link time via -ldflags.
var (
	version = "0.1.0"
	commit  = "unknown"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "kura-tape-backup:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	opts, err := parseArgs(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if opts.showVersion {
		fmt.Fprintln(stdout, version)
		return nil
	}

	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}
	level, err := parseLogLevel(cfg.Server.LogLevel)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	attrs := []any{
		"service", "kura-tape-backup",
		"mode", opts.command,
		"version", version,
		"commit", commit,
	}
	switch opts.command {
	case "serve":
		attrs = append(attrs, "addr", cfg.Server.Addr)
	case "run":
		attrs = append(
			attrs,
			"ltfs_root", cfg.Tape.LTFSRoot,
			"drive_device", cfg.Tape.DriveDevice,
		)
	}
	logger.Info("kura tape backup starting", attrs...)

	return fmt.Errorf("%s is not implemented", opts.command)
}

type options struct {
	command     string
	configPath  string
	showVersion bool
}

func parseArgs(args []string, stderr io.Writer) (options, error) {
	if len(args) > 0 && isCommand(args[0]) {
		opts, remaining, err := parseFlags(args[0], args[1:], stderr)
		if err != nil {
			return options{}, err
		}
		if len(remaining) != 0 {
			return options{}, fmt.Errorf("unexpected positional arguments: %v", remaining)
		}
		opts.command = args[0]
		return opts, nil
	}

	opts, remaining, err := parseFlags("kura-tape-backup", args, stderr)
	if err != nil {
		return options{}, err
	}
	if opts.showVersion {
		return opts, nil
	}
	if len(remaining) != 1 || !isCommand(remaining[0]) {
		return options{}, fmt.Errorf("expected subcommand serve or run")
	}
	opts.command = remaining[0]
	return opts, nil
}

func parseFlags(name string, args []string, stderr io.Writer) (options, []string, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", config.DefaultPath, "TOML configuration file")
	showVersion := flags.Bool("version", false, "print version and exit")
	if err := flags.Parse(args); err != nil {
		return options{}, nil, err
	}
	return options{
		configPath:  *configPath,
		showVersion: *showVersion,
	}, flags.Args(), nil
}

func isCommand(arg string) bool {
	return arg == "serve" || arg == "run"
}

func parseLogLevel(value string) (slog.Level, error) {
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q", value)
	}
}

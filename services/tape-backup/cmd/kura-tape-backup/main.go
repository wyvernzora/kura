// Command kura-tape-backup provides the tape archive service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/wyvernzora/kura/services/tape-backup/internal/config"
)

// version and commit are overridable at link time via -ldflags.
var (
	version = "0.1.0"
	commit  = "unknown"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "kura-tape-backup:", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	args []string,
	getenv func(string) string,
	stdout io.Writer,
	stderr io.Writer,
) error {
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
	return runServer(ctx, cfg, getenv, stderr)
}

type options struct {
	command     string
	configPath  string
	showVersion bool
}

func parseArgs(args []string, stderr io.Writer) (options, error) {
	if len(args) > 0 && args[0] == "serve" {
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
	if len(remaining) != 1 || remaining[0] != "serve" {
		return options{}, fmt.Errorf("expected subcommand serve")
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

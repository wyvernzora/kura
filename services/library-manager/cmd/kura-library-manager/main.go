package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

const defaultConfigPath = "/etc/kura/library-manager.toml"

var errFlagAlreadyReported = errors.New("flag error already reported")

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, errFlagAlreadyReported) {
			fmt.Fprintln(os.Stderr, err)
		}
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
	flags := flag.NewFlagSet("kura-library-manager", flag.ContinueOnError)
	flags.SetOutput(stderr)

	opts := serverOptions{}
	flags.StringVar(&opts.Config, "config", defaultConfigPath, "load serve settings from a strict TOML file")
	printVersion := flags.Bool("version", false, "print the kura library-manager version and exit")
	flags.BoolVar(&opts.UseTestStubs, "use-test-stubs", false, "test only: use the e2e stub provider and inspector")
	flags.StringVar(&opts.StubProviderFixture, "stub-provider-fixture", "", "test only: load the e2e stub provider fixture from this path")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errFlagAlreadyReported
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *printVersion {
		_, err := fmt.Fprintln(stdout, Version)
		return err
	}
	return runServer(ctx, opts, getenv, stderr)
}

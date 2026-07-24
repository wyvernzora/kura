package main

import (
	"context"
	"io"
	"os"

	"github.com/alecthomas/kong"
	"github.com/wyvernzora/kura/services/library-manager/internal/cli/stdio"
)

// runContext is the per-invocation harness bound to kong as a pointer so
// run() can enrich Context after flag parsing. After run() returns from
// parser.Parse, Context carries:
//   - stdio.Stdio (always; via stdio.With)
//
// Commands receive *runContext via kong.Bind and read stdio via stdio.From.
type runContext struct {
	Context context.Context
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
}

func run(args []string, rt runContext) error {
	if rt.Context == nil {
		rt.Context = context.Background()
	}
	if rt.Stdin == nil {
		rt.Stdin = os.Stdin
	}
	if rt.Stdout == nil {
		rt.Stdout = io.Discard
	}
	if rt.Stderr == nil {
		rt.Stderr = io.Discard
	}
	if rt.Getenv == nil {
		rt.Getenv = os.Getenv
	}

	flags := &cli{}
	parser, err := kong.New(flags,
		kong.Name("kura-library-manager"),
		kong.Description("Anime-first library manager."),
		kong.Bind(&rt),
		kong.Writers(rt.Stdout, rt.Stderr),
	)
	if err != nil {
		return err
	}

	kctx, err := parser.Parse(args)
	if err != nil {
		return err
	}

	rt.Context = stdio.With(rt.Context, stdio.New(rt.Stdin, rt.Stdout, rt.Stderr))
	return kctx.Run()
}

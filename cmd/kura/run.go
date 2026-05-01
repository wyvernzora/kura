package main

import (
	"context"
	"io"
	"os"

	"github.com/alecthomas/kong"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/resolve"
	"github.com/wyvernzora/kura/internal/ui"
	"github.com/wyvernzora/kura/internal/ui/stdio"
)

// runContext is the per-invocation harness bound to kong as a pointer so
// run() can enrich Context after flag parsing. After run() returns from
// parser.Parse, Context carries:
//   - stdio.Stdio          (always; via stdio.With)
//   - progress.Reporter    (always; disabled automatically for non-terminals)
//   - lazy metadata.Source (via metadata.WithSource)
//   - lazy *resolve.Resolver (via resolve.WithResolver)
//
// Commands receive *runContext via kong.Bind and read these via stdio.From,
// metadata.SourceFrom, and resolve.ResolverFrom respectively.
type runContext struct {
	Context context.Context
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
	flags   *cli
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
	rt.flags = flags
	parser, err := kong.New(flags,
		kong.Name("kura"),
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
	rt.Context = progress.With(rt.Context, ui.NewProgressReporter(rt.Stderr))
	rt.Context = metadata.WithSource(rt.Context, func() (metadata.Source, error) {
		return buildSourceFromFlags(&rt, flags)
	})
	rt.Context = resolve.WithResolver(rt.Context, func() (*resolve.Resolver, error) {
		src, err := metadata.SourceFrom(rt.Context)
		if err != nil {
			return nil, err
		}
		return resolve.New(
			resolve.NewMetadataIDStrategy(src),
			resolve.NewTextSearchStrategy(src),
		), nil
	})
	return kctx.Run()
}

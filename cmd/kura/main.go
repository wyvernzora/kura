package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kong"
)

type cli struct {
	Meta    metaCmd    `cmd:"" help:"Metadata provider commands."`
	Library libraryCmd `cmd:"" help:"Library metadata commands."`
	Series  seriesCmd  `cmd:"" help:"Series metadata commands."`
	Episode episodeCmd `cmd:"" help:"Episode metadata commands."`
}

type runContext struct {
	Context context.Context
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
}

func main() {
	err := run(os.Args[1:], runContext{
		Context: context.Background(),
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Getenv:  os.Getenv,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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

	parser, err := kong.New(&cli{},
		kong.Name("kura"),
		kong.Description("Anime-first library manager."),
		kong.Bind(rt),
		kong.Writers(rt.Stdout, rt.Stderr),
	)
	if err != nil {
		return err
	}

	ctx, err := parser.Parse(args)
	if err != nil {
		return err
	}
	return ctx.Run()
}

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/wyvernzora/kura/internal/config"
	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/store"
)

type cli struct {
	Sync      seriesSyncCmd      `cmd:"" help:"Scan a series and sync it into Kura metadata."`
	Reconcile seriesReconcileCmd `cmd:"" help:"Rename tracked files to match Kura metadata."`
	Stage     stageCmd           `cmd:"" help:"Stage an external episode file for a series."`
	Meta      metaCmd            `cmd:"" help:"Metadata provider commands."`
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

func buildMetadataSource(rt runContext, providerKey string, tvdbBaseURL string) (metadata.Source, error) {
	return config.BuildMetadataSource(config.MetadataSourceOptions{
		Key:         providerKey,
		TVDBBaseURL: tvdbBaseURL,
		Getenv:      rt.Getenv,
	})
}

func mediaInspector(rt runContext) mediainfo.Inspector {
	inspector := mediainfo.New()
	command := strings.TrimSpace(rt.Getenv("KURA_MEDIAINFO_COMMAND"))
	if command != "" {
		inspector.Command = command
	}
	return inspector
}

func parseRemoteSeriesRef(seriesRef string) (string, string, error) {
	ref, err := domain.ParseRemoteSeriesRef(seriesRef)
	if err != nil {
		return "", "", err
	}
	if ref.Source() != "tvdb" {
		return "", "", fmt.Errorf("unsupported series ref provider %q; only tvdb:<id> is supported", ref.Source())
	}
	return ref.Source(), ref.ID(), nil
}

func providerRefForSource(series store.Series, source string) (domain.RemoteSeriesRef, error) {
	for _, raw := range series.ProviderRefs {
		ref, err := domain.ParseRemoteSeriesRef(raw)
		if err != nil {
			return domain.RemoteSeriesRef{}, err
		}
		if ref.Source() == source {
			return ref, nil
		}
	}
	return domain.RemoteSeriesRef{}, fmt.Errorf("series has no %s provider ref", source)
}

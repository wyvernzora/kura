package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/wyvernzora/kura/internal/config"
	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/ops"
	"github.com/wyvernzora/kura/internal/store"
	"github.com/wyvernzora/kura/internal/ui"
)

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

func providerSeriesResolver(rt runContext, providerKey string, tvdbBaseURL string) ops.ProviderSeriesResolver {
	return func(ctx context.Context, local store.Series) (metadata.Series, error) {
		metadataSource, err := buildMetadataSource(rt, providerKey, tvdbBaseURL)
		if err != nil {
			return metadata.Series{}, err
		}
		ref, err := providerRefForSource(local, metadataSource.Key())
		if err != nil {
			return metadata.Series{}, err
		}
		return metadataSource.GetSeries(ctx, ref.ID())
	}
}

func isInteractiveRun(rt runContext) bool {
	stdin, stdinOK := rt.Stdin.(*os.File)
	stdout, stdoutOK := rt.Stdout.(*os.File)
	return stdinOK && stdoutOK && ui.IsTerminal(stdin) && ui.IsTerminal(stdout)
}

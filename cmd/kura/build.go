package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/wyvernzora/kura/internal/config"
	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/ops"
	"github.com/wyvernzora/kura/internal/store"
	"github.com/wyvernzora/kura/internal/ui/stdio"
)

// buildSourceFromFlags constructs the metadata source from the global CLI
// flags. Used by run.go to seed the lazy metadata.WithSource builder.
func buildSourceFromFlags(rt *runContext, flags *cli) (metadata.Source, error) {
	return config.BuildMetadataSource(config.MetadataSourceOptions{
		TVDBBaseURL: flags.TVDBBaseURL,
		Getenv:      rt.Getenv,
	})
}

func mediaInspector(rt *runContext) mediainfo.Inspector {
	inspector := mediainfo.New()
	command := strings.TrimSpace(rt.Getenv("KURA_MEDIAINFO_COMMAND"))
	if command != "" {
		inspector.Command = command
	}
	return inspector
}

func parseMetadataRef(seriesRef string) (string, string, error) {
	ref, err := domain.ParseMetadataRef(seriesRef)
	if err != nil {
		return "", "", err
	}
	if ref.Source() != "tvdb" {
		return "", "", fmt.Errorf("unsupported metadata ref source %q; only tvdb:<id> is supported", ref.Source())
	}
	return ref.Source(), ref.ID(), nil
}

func metadataRefForSource(series store.Series, source string) (domain.MetadataRef, error) {
	ref, err := domain.ParseMetadataRef(series.MetadataRef)
	if err != nil {
		return domain.MetadataRef{}, err
	}
	if ref.Source() != source {
		return domain.MetadataRef{}, fmt.Errorf("series metadata ref source %q does not match %q", ref.Source(), source)
	}
	return ref, nil
}

func metadataSeriesResolver(rt *runContext) ops.MetadataSeriesResolver {
	return func(ctx context.Context, local store.Series) (metadata.Series, error) {
		metadataSource, err := metadata.SourceFrom(rt.Context)
		if err != nil {
			return metadata.Series{}, err
		}
		ref, err := metadataRefForSource(local, metadataSource.Key())
		if err != nil {
			return metadata.Series{}, err
		}
		return metadataSource.GetSeries(ctx, ref.ID())
	}
}

func isInteractiveRun(rt *runContext) bool {
	return stdio.From(rt.Context).IsInteractive()
}

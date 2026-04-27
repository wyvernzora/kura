package main

import (
	"fmt"

	"github.com/wyvernzora/kura/internal/config"
	"github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/metadata"
)

func buildMetadataSource(rt runContext, providerKey string, tvdbBaseURL string) (metadata.Source, error) {
	return config.BuildMetadataSource(config.MetadataSourceOptions{
		Key:         providerKey,
		TVDBBaseURL: tvdbBaseURL,
		Getenv:      rt.Getenv,
	})
}

func parseRemoteSeriesRef(seriesRef string) (string, string, error) {
	ref, err := metadata.ParseRemoteSeriesRef(seriesRef)
	if err != nil {
		return "", "", err
	}
	if ref.Source() != "tvdb" {
		return "", "", fmt.Errorf("unsupported series ref provider %q; only tvdb:<id> is supported", ref.Source())
	}
	return ref.Source(), ref.ID(), nil
}

func providerRefForSource(series library.Series, source string) (metadata.RemoteSeriesRef, error) {
	for _, raw := range series.ProviderRefs {
		ref, err := metadata.ParseRemoteSeriesRef(raw)
		if err != nil {
			return metadata.RemoteSeriesRef{}, err
		}
		if ref.Source() == source {
			return ref, nil
		}
	}
	return metadata.RemoteSeriesRef{}, fmt.Errorf("series has no %s provider ref", source)
}

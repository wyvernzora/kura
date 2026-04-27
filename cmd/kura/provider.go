package main

import (
	"fmt"

	"github.com/wyvernzora/kura/internal/config"
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

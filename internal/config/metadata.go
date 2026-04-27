package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/metadata/tvdb"
)

type EnvFunc func(string) string

type MetadataSourceOptions struct {
	Key         string
	TVDBBaseURL string
	Getenv      EnvFunc
}

func BuildMetadataSource(opts MetadataSourceOptions) (metadata.Source, error) {
	key := opts.Key
	if key == "" {
		key = "tvdb"
	}
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}

	switch key {
	case "tvdb":
		apiKey := getenv("KURA_TVDB_KEY")
		if apiKey == "" {
			return nil, errors.New("KURA_TVDB_KEY is required for TVDB metadata requests")
		}
		preferredLanguages, err := ParsePreferredLanguages(getenv("KURA_PREFERRED_LANGUAGES"))
		if err != nil {
			return nil, err
		}
		p, err := tvdb.New(apiKey, tvdb.Options{
			BaseURL:            opts.TVDBBaseURL,
			PreferredLanguages: preferredLanguages.Tags(),
		})
		if err != nil {
			return nil, err
		}
		return metadata.NewCachedSource(p, metadata.CacheOptions{})
	default:
		return nil, fmt.Errorf("unsupported metadata provider %q", key)
	}
}

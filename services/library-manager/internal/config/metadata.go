package config

import (
	"errors"
	"os"

	"github.com/wyvernzora/kura/services/library-manager/internal/provider"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider/tvdb"
)

type EnvFunc func(string) string

type MetadataSourceOptions struct {
	TVDBBaseURL string
	Getenv      EnvFunc
}

func BuildMetadataSource(opts MetadataSourceOptions) (provider.Source, error) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}

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
	return provider.NewCachedSource(p, provider.CacheOptions{})
}

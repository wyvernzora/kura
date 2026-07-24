package config

import (
	"errors"
	"slices"

	"github.com/wyvernzora/kura/services/library-manager/internal/provider"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider/tvdb"
)

type MetadataSourceOptions struct {
	APIKey             string
	TVDBURL            string
	PreferredLanguages []string
}

func BuildMetadataSource(opts MetadataSourceOptions) (provider.Source, error) {
	if opts.APIKey == "" {
		return nil, errors.New("KURA_TVDB_KEY is required for TVDB metadata requests")
	}
	p, err := tvdb.New(opts.APIKey, tvdb.Options{
		BaseURL:            opts.TVDBURL,
		PreferredLanguages: slices.Clone(opts.PreferredLanguages),
	})
	if err != nil {
		return nil, err
	}
	return provider.NewCachedSource(p, provider.CacheOptions{})
}

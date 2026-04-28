package main

import (
	"encoding/json"
	"fmt"

	"github.com/wyvernzora/kura/internal/resolve"
)

type metaCmd struct {
	Search metaSearchCmd `cmd:"" help:"Search metadata providers."`
	Get    metaGetCmd    `cmd:"" help:"Fetch metadata for a provider series reference."`
}

type metaSearchCmd struct {
	Provider    string   `help:"Metadata provider to use." enum:"tvdb" default:"tvdb"`
	Limit       int      `help:"Maximum number of resolver results to print. Zero prints all results."`
	TVDBBaseURL string   `name:"tvdb-base-url" hidden:"" help:"Override the TVDB API base URL."`
	Terms       []string `arg:"" required:"" help:"Resolver terms. Use plain text or provider refs such as tvdb:370070."`
}

type metaGetCmd struct {
	TVDBBaseURL string `name:"tvdb-base-url" hidden:"" help:"Override the TVDB API base URL."`
	SeriesRef   string `arg:"" help:"Provider series reference. Only tvdb:<id> is supported."`
}

func (cmd *metaSearchCmd) Run(rt runContext) error {
	if cmd.Limit < 0 {
		return fmt.Errorf("--limit must be greater than or equal to zero")
	}

	metadataSource, err := buildMetadataSource(rt, cmd.Provider, cmd.TVDBBaseURL)
	if err != nil {
		return err
	}

	resolver := resolve.New(
		resolve.NewProviderIDStrategy(metadataSource),
		resolve.NewTextSearchStrategy(metadataSource),
	)
	results, err := resolver.Resolve(rt.Context, resolve.ParseQuery(cmd.Terms))
	if err != nil {
		return err
	}
	if cmd.Limit > 0 && len(results.Results) > cmd.Limit {
		results.Results = results.Results[:cmd.Limit]
	}

	encoder := json.NewEncoder(rt.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(results)
}

func (cmd *metaGetCmd) Run(rt runContext) error {
	providerKey, providerID, err := parseRemoteSeriesRef(cmd.SeriesRef)
	if err != nil {
		return err
	}

	metadataSource, err := buildMetadataSource(rt, providerKey, cmd.TVDBBaseURL)
	if err != nil {
		return err
	}

	series, err := metadataSource.GetSeries(rt.Context, providerID)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(rt.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(series)
}

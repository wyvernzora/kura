package main

import (
	"encoding/json"
	"fmt"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/resolve"
)

type metaCmd struct {
	Search metaSearchCmd `cmd:"" help:"Search metadata providers."`
	Get    metaGetCmd    `cmd:"" help:"Fetch metadata for a provider series reference."`
}

type metaSearchCmd struct {
	Limit int      `help:"Maximum number of resolver results to print. Zero prints all results."`
	Terms []string `arg:"" required:"" help:"Resolver terms. Use plain text or provider refs such as tvdb:370070."`
}

type metaGetCmd struct {
	SeriesRef string `arg:"" help:"Provider series reference. Only tvdb:<id> is supported."`
}

func (cmd *metaSearchCmd) Run(rt *runContext) error {
	if cmd.Limit < 0 {
		return fmt.Errorf("--limit must be greater than or equal to zero")
	}

	resolver, err := resolve.ResolverFrom(rt.Context)
	if err != nil {
		return err
	}
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

func (cmd *metaGetCmd) Run(rt *runContext) error {
	providerKey, providerID, err := parseRemoteSeriesRef(cmd.SeriesRef)
	if err != nil {
		return err
	}

	metadataSource, err := metadata.SourceFrom(rt.Context)
	if err != nil {
		return err
	}
	if metadataSource.Key() != providerKey {
		return fmt.Errorf("configured metadata provider %q cannot fetch %s series refs", metadataSource.Key(), providerKey)
	}

	series, err := metadataSource.GetSeries(rt.Context, providerID)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(rt.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(series)
}

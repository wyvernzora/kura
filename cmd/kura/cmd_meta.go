package main

import (
	"encoding/json"
	"fmt"

	"github.com/wyvernzora/kura/internal/domain/selector"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/resolve"
)

type metaCmd struct {
	Search metaSearchCmd `cmd:"" help:"Search metadata."`
	Get    metaGetCmd    `cmd:"" help:"Fetch metadata for a series reference."`
}

type metaSearchCmd struct {
	Limit int      `help:"Maximum number of resolver results to print. Zero prints all results."`
	Terms []string `arg:"" required:"" help:"Resolver terms. Use plain text or metadata refs such as tvdb:370070."`
}

type metaGetCmd struct {
	SeriesRef string `arg:"" help:"Metadata series reference. Only tvdb:<id> is supported."`
}

func (cmd *metaSearchCmd) Run(rt *runContext) error {
	if cmd.Limit < 0 {
		return fmt.Errorf("--limit must be greater than or equal to zero")
	}

	resolver, err := resolve.ResolverFrom(rt.Context)
	if err != nil {
		return err
	}
	results, err := resolver.Resolve(rt.Context, selector.ParseSelector(cmd.Terms))
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
	sourceKey, metadataID, err := parseMetadataRef(cmd.SeriesRef)
	if err != nil {
		return err
	}

	metadataSource, err := metadata.SourceFrom(rt.Context)
	if err != nil {
		return err
	}
	if metadataSource.Key() != sourceKey {
		return fmt.Errorf("configured metadata source %q cannot fetch %s series refs", metadataSource.Key(), sourceKey)
	}

	series, err := metadataSource.GetSeries(rt.Context, metadataID)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(rt.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(series)
}

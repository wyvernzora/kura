package workflow

import (
	"context"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/response"
)

// MetadataAliasesInput parameters for the MetadataAliases workflow.
type MetadataAliasesInput struct {
	Ref string
}

// MetadataAliases returns all known titles and aliases for a series from
// the configured metadata provider. Results include official translated
// titles and provider-supplied alternate names, each tagged with a BCP-47
// language code (empty when the provider does not tag the entry).
//
// Provider-needing: invokes deps.Provider() lazily.
func MetadataAliases(ctx context.Context, deps Deps, in MetadataAliasesInput) (response.SeriesAliases, error) {
	ref, err := refs.ParseMetadata(in.Ref)
	if err != nil {
		return response.SeriesAliases{}, err
	}

	source, err := deps.Provider()
	if err != nil {
		return response.SeriesAliases{}, err
	}

	series, err := source.GetSeries(ctx, ref.ID(), "")
	if err != nil {
		return response.SeriesAliases{}, err
	}

	seen := make(map[string]struct{})
	var entries []response.AliasEntry
	add := func(lang, value string) {
		key := lang + "\x00" + value
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			entries = append(entries, response.AliasEntry{Lang: lang, Alias: value})
		}
	}
	for _, t := range series.TranslatedTitles {
		add(t.Language, t.Value)
	}
	for _, a := range series.Aliases {
		add(a.Language, a.Value)
	}

	return response.SeriesAliases{Ref: in.Ref, Aliases: entries}, nil
}

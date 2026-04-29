package main

import (
	"encoding/json"
	"fmt"

	"github.com/wyvernzora/kura/internal/config"
	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/kura"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/ui"
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

func libraryFromFlags(rt *runContext, flags *cli) (*kura.Library, error) {
	preferredLanguages, err := config.ParsePreferredLanguages(rt.Getenv("KURA_PREFERRED_LANGUAGES"))
	if err != nil {
		return nil, err
	}
	return kura.New(kura.Config{
		Root:               rt.Getenv("KURA_LIBRARY_ROOT"),
		MediainfoCommand:   rt.Getenv("KURA_MEDIAINFO_COMMAND"),
		TVDBKey:            rt.Getenv("KURA_TVDB_KEY"),
		TVDBBaseURL:        flags.TVDBBaseURL,
		PreferredLanguages: preferredLanguages.Tags(),
	})
}

func resolveMetadataRef(rt *runContext, lib *kura.Library, terms []string) (kura.MetadataRef, error) {
	resolution, err := lib.Resolve(rt.Context, kura.ResolveInput{Terms: terms})
	if err != nil {
		return "", err
	}
	picked, err := ui.SelectFromResolution(stdio.From(rt.Context), resolution, terms)
	if err != nil {
		return "", err
	}
	return kura.MetadataRef(picked.Summary.MetadataRef), nil
}

func writeSeriesSummary(rt *runContext, series *kura.Series, verb string, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(rt.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(series)
	}
	_, err := fmt.Fprintf(rt.Stdout, "%s %s (%s)\n", verb, series.Ref(), series.MetadataRef())
	return err
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

package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/wyvernzora/kura/internal/config"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/series"
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

func libraryFromFlags(rt *runContext, flags *cli) (*library.Library, error) {
	preferredLanguages, err := config.ParsePreferredLanguages(rt.Getenv("KURA_PREFERRED_LANGUAGES"))
	if err != nil {
		return nil, err
	}
	return library.Open(library.Config{
		Root:               rt.Getenv("KURA_LIBRARY_ROOT"),
		MediainfoCommand:   rt.Getenv("KURA_MEDIAINFO_COMMAND"),
		TVDBKey:            rt.Getenv("KURA_TVDB_KEY"),
		TVDBBaseURL:        flags.TVDBBaseURL,
		PreferredLanguages: preferredLanguages.Tags(),
		Context:            rt.Context,
	})
}

func resolveMetadataRef(rt *runContext, lib *library.Library, terms []string) (refs.Metadata, error) {
	resolution, err := lib.Resolve(rt.Context, terms)
	if err != nil {
		return "", err
	}
	picked, err := ui.SelectFromResolution(stdio.From(rt.Context), resolution, terms)
	if err != nil {
		return "", err
	}
	return refs.Metadata(picked.Summary.MetadataRef), nil
}

func resolveSeriesHandle(rt *runContext, terms []string) (series.Handle, error) {
	lib, err := libraryFromFlags(rt, rt.flags)
	if err != nil {
		return series.Handle{}, err
	}
	metadataRef, err := resolveMetadataRef(rt, lib, terms)
	if err != nil {
		return series.Handle{}, err
	}
	return lib.Find(metadataRef)
}

func writeSeriesSummary(rt *runContext, handle series.Handle, verb string, asJSON bool) error {
	view, err := handle.Read(rt.Context, series.ReadInput{})
	if err != nil {
		return err
	}
	if asJSON {
		encoder := json.NewEncoder(rt.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(view)
	}
	_, err = fmt.Fprintf(rt.Stdout, "%s %s (%s)\n", verb, handle.Ref(), view.MetadataRef)
	return err
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func parseMetadataRef(seriesRef string) (string, string, error) {
	ref, err := refs.ParseMetadata(seriesRef)
	if err != nil {
		return "", "", fmt.Errorf("invalid metadata ref %q; expected <source>:<id>", seriesRef)
	}
	if ref.Provider() != "tvdb" {
		return "", "", fmt.Errorf("unsupported metadata ref source %q; only tvdb:<id> is supported", ref.Provider())
	}
	return ref.Provider(), ref.ID(), nil
}

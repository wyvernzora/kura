package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/terminalui"
)

type episodeCmd struct {
	Import episodeImportCmd `cmd:"" help:"Import an existing episode file into .kura/series.json."`
}

type episodeImportCmd struct {
	Season      int      `help:"Season number."`
	Special     bool     `help:"Import as a special."`
	Number      int      `help:"Episode number." required:""`
	Provider    string   `help:"Metadata provider to validate against." enum:"tvdb" default:"tvdb"`
	TVDBBaseURL string   `name:"tvdb-base-url" hidden:"" help:"Override the TVDB API base URL."`
	Source      string   `help:"Media source. Defaults to filename source or unknown."`
	Companions  []string `name:"companion" help:"Companion file path relative to KURA_LIBRARY_ROOT."`
	DryRun      bool     `name:"dry-run" help:"Print the updated series document without writing it."`
	Replace     bool     `name:"replace" help:"Replace an existing episode record, moving the old record to trash."`
	Path        string   `arg:"" help:"Media file path relative to KURA_LIBRARY_ROOT."`
}

func (cmd *episodeImportCmd) Run(rt runContext) error {
	if cmd.Special && cmd.Season != 0 {
		return errors.New("--season and --special are mutually exclusive")
	}
	if !cmd.Special && cmd.Season < 1 {
		return errors.New("--season is required unless --special is set")
	}

	season := library.SpecialsSeason()
	var err error
	if !cmd.Special {
		season, err = library.RegularSeason(cmd.Season)
		if err != nil {
			return err
		}
	}
	episode, err := library.NewEpisodeNumber(cmd.Number)
	if err != nil {
		return errors.New("--number must be greater than zero")
	}
	root, err := library.ParseLibraryRoot(rt.Getenv("KURA_LIBRARY_ROOT"))
	if err != nil {
		return err
	}

	source := library.MediaSource("")
	if strings.TrimSpace(cmd.Source) != "" {
		source = library.ParseMediaSource(cmd.Source)
	}
	updated, err := library.New().ImportEpisodeFile(
		library.WithProgress(rt.Context, terminalui.NewProgressReporter(rt.Stderr)),
		root,
		library.ImportEpisodeFileOptions{
			Season:           season,
			Episode:          episode,
			Source:           source,
			Companions:       cmd.Companions,
			MediaPath:        cmd.Path,
			Inspector:        mediaInspector(rt),
			ProviderResolver: episodeProviderSeriesResolver(rt, cmd.Provider, cmd.TVDBBaseURL),
			Apply:            !cmd.DryRun,
			Replace:          cmd.Replace,
		},
	)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(rt.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(updated)
}

func episodeProviderSeriesResolver(rt runContext, provider string, tvdbBaseURL string) library.ProviderSeriesResolver {
	return func(ctx context.Context, local library.Series) (metadata.Series, error) {
		metadataSource, err := buildMetadataSource(rt, provider, tvdbBaseURL)
		if err != nil {
			return metadata.Series{}, err
		}
		ref, err := providerRefForSource(local, metadataSource.Key())
		if err != nil {
			return metadata.Series{}, err
		}
		return metadataSource.GetSeries(ctx, ref.ID())
	}
}

func mediaInspector(rt runContext) mediainfo.Inspector {
	inspector := mediainfo.New()
	command := strings.TrimSpace(rt.Getenv("KURA_MEDIAINFO_COMMAND"))
	if command != "" {
		inspector.Command = command
	}
	return inspector
}

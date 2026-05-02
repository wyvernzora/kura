package main

import (
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/selector"
	"github.com/wyvernzora/kura/internal/library"
)

type importCmd struct {
	Dirname string   `arg:"" required:"" help:"Existing directory below KURA_LIBRARY_ROOT."`
	JSON    bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Force   bool     `name:"force" help:"Replace existing .kura/series.json while preserving other .kura contents."`
	Terms   []string `arg:"" optional:"" help:"Additional resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *importCmd) Run(rt *runContext) error {
	lib, err := libraryFromFlags(rt, rt.flags)
	if err != nil {
		return err
	}
	terms, err := cmd.resolveTerms()
	if err != nil {
		return err
	}
	metadataRef, err := resolveMetadataRef(rt, lib, terms)
	if err != nil {
		return err
	}
	ref, err := refs.ParseSeries(cmd.Dirname)
	if err != nil {
		return err
	}
	series, err := lib.Import(rt.Context, library.ImportInput{
		Ref:      ref,
		Metadata: metadataRef,
		Force:    cmd.Force,
	})
	if err != nil {
		return err
	}
	return writeSeriesSummary(rt, series, "Imported", cmd.JSON)
}

func (cmd *importCmd) resolveTerms() ([]string, error) {
	tvdbTerms := 0
	nonEmptyTerms := 0
	var tvdbTerm string

	for _, raw := range cmd.Terms {
		term := selector.ParseTerm(raw)
		if term == "" {
			continue
		}
		nonEmptyTerms++
		ref, err := refs.ParseMetadata(term.String())
		if err == nil && ref.Provider() == "tvdb" {
			tvdbTerms++
			tvdbTerm = raw
		}
	}

	if tvdbTerms == 1 && nonEmptyTerms == 1 {
		return []string{tvdbTerm}, nil
	}
	if nonEmptyTerms > 0 && tvdbTerms > 0 {
		return cmd.Terms, nil
	}

	terms := make([]string, 0, len(cmd.Terms)+1)
	terms = append(terms, cmd.Dirname)
	terms = append(terms, cmd.Terms...)
	return terms, nil
}

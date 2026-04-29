package main

import (
	"errors"

	"github.com/wyvernzora/kura/internal/kura"
	"github.com/wyvernzora/kura/internal/resolve"
)

var errImportDirTermUnsupported = errors.New("kura import: dir: resolver terms are not supported")

type importCmd struct {
	Dirname string   `arg:"" required:"" help:"Existing directory below KURA_LIBRARY_ROOT."`
	JSON    bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
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
	series, err := lib.Import(rt.Context, kura.ImportInput{
		Ref:         kura.SeriesRef(cmd.Dirname),
		MetadataRef: metadataRef,
	})
	if err != nil {
		return err
	}
	return writeSeriesSummary(rt, series, "Imported", cmd.JSON)
}

func (cmd *importCmd) resolveTerms() ([]string, error) {
	tvdbTerms := 0
	otherPrefixedTerms := 0
	nonEmptyTerms := 0
	var tvdbTerm string

	for _, raw := range cmd.Terms {
		term := resolve.ParseTerm(raw)
		if term == (resolve.Term{}) {
			continue
		}
		nonEmptyTerms++
		switch term.Prefix {
		case "":
		case "dir":
			return nil, errImportDirTermUnsupported
		case "tvdb":
			tvdbTerms++
			tvdbTerm = raw
		default:
			otherPrefixedTerms++
		}
	}

	if tvdbTerms == 1 && nonEmptyTerms == 1 {
		return []string{tvdbTerm}, nil
	}
	if nonEmptyTerms > 0 && (tvdbTerms > 0 || otherPrefixedTerms > 0) {
		return cmd.Terms, nil
	}

	terms := make([]string, 0, len(cmd.Terms)+1)
	terms = append(terms, cmd.Dirname)
	terms = append(terms, cmd.Terms...)
	return terms, nil
}

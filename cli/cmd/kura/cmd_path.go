package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wyvernzora/kura/cli/internal/cli/client"
	"github.com/wyvernzora/kura/cli/internal/cli/stdio"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

type pathCmd struct {
	SeriesFile bool     `name:"seriesfile" help:"Print the path to series.json instead of the series root."`
	Terms      []string `arg:"" required:"" help:"Resolver terms. Plain text or metadata refs such as tvdb:370070."`
}

func (cmd *pathCmd) Run(rt *runContext) error {
	libRoot := rt.Getenv("KURA_LIBRARY_ROOT")
	if libRoot == "" {
		return errors.New("KURA_LIBRARY_ROOT is required")
	}
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, cmd.Terms)
	if err != nil {
		return err
	}
	show, err := c.ShowSeries(rt.Context, ref, client.ShowOptions{})
	if err != nil {
		return err
	}
	rel, err := libraryRel(show.Root)
	if err != nil {
		return err
	}
	out := filepath.Join(libRoot, filepath.FromSlash(rel))
	if cmd.SeriesFile {
		out = filepath.Join(out, api.KuraDir, api.SeriesFileName)
	}
	_, err = fmt.Fprintln(rt.Stdout, out)
	return err
}

func libraryRel(selector string) (string, error) {
	scheme, rel, ok := strings.Cut(selector, ":")
	if !ok || scheme != "library" || rel == "" {
		return "", fmt.Errorf("expected library selector, got %q", selector)
	}
	return rel, nil
}

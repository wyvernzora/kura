package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/wyvernzora/kura/services/library-manager/internal/cli/client"
	"github.com/wyvernzora/kura/services/library-manager/internal/cli/render"
	"github.com/wyvernzora/kura/services/library-manager/internal/cli/stdio"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/selector"
	"github.com/wyvernzora/kura/services/library-manager/internal/response"
)

// stageCmd is a parent verb with three subcommands: episode, trash, extra.
// CLI is intentionally one-item-per-call; multi-item batching lives only
// on the MCP/REST surface.
type stageCmd struct {
	Episode stageEpisodeCmd `cmd:"" name:"episode" aliases:"ep" help:"Stage one media file for an episode slot."`
	Trash   stageTrashCmd   `cmd:"" name:"trash" help:"Queue one file for trash on next reconcile."`
	Extra   stageExtraCmd   `cmd:"" name:"extra" aliases:"ex" help:"Queue one file or directory as a season Extra on next reconcile."`
}

type stageEpisodeCmd struct {
	Source     string   `help:"Media source. Defaults to filename source or unknown."`
	Companions []string `name:"companion" help:"Companion inbox selector (e.g. inbox:[BDrip] Show/E03.en.srt)."`
	Attrs      []string `name:"attr" help:"Media attr key=value. May be repeated."`
	Replace    bool     `name:"replace" help:"Stage over an active episode or replace an existing staged entry for the same season and episode."`
	JSON       bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Args       []string `arg:"" required:"" help:"Selector terms, episode marker (e.g. S01E03), and an inbox: media selector, or series: for in-place metadata override."`
}

type stageTrashCmd struct {
	Companions []string `name:"companion" help:"Companion series: selector (e.g. series:Season 1/foo.en.srt)."`
	JSON       bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Args       []string `arg:"" required:"" help:"Selector terms followed by a series: selector for the file to queue for trash."`
}

type stageExtraCmd struct {
	Season int      `name:"season" required:"" help:"Season number under which to place the extra."`
	Prefix string   `name:"prefix" help:"Optional sub-folder name under Season N/Extra/."`
	JSON   bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Args   []string `arg:"" required:"" help:"Selector terms followed by an inbox: selector (use 'kura inbox list' to discover)."`
}

// stageRequestBody mirrors the REST stage payload. CLI builds a single-
// item batch (one episode / trash / extra entry per invocation); REST
// handler accepts the multi-item shape that MCP also uses.
type stageRequestBody struct {
	Episodes []stageRequestEpisode `json:"episodes,omitempty"`
	Trash    []stageRequestTrash   `json:"trash,omitempty"`
	Extras   []stageRequestExtra   `json:"extras,omitempty"`
}

type stageRequestEpisode struct {
	Episode    string            `json:"episode"`
	Media      string            `json:"media"`
	Source     string            `json:"source,omitempty"`
	Companions []string          `json:"companions,omitempty"`
	Replace    bool              `json:"replace,omitempty"`
	Attrs      map[string]string `json:"attrs,omitempty"`
}

type stageRequestTrash struct {
	Path       string   `json:"path"`
	Companions []string `json:"companions,omitempty"`
}

type stageRequestExtra struct {
	Season int    `json:"season"`
	Source string `json:"source"`
	Prefix string `json:"prefix,omitempty"`
}

func (cmd *stageEpisodeCmd) Run(rt *runContext) error {
	terms, episodeRaw, mediaArg, err := splitStageEpisodeArgs(cmd.Args)
	if err != nil {
		return err
	}
	episode, err := refs.ParseEpisodeMarker(episodeRaw)
	if err != nil {
		return err
	}
	mediaSel, err := parseStageMediaArg(mediaArg)
	if err != nil {
		return err
	}
	companions, err := parseStageCompanionSelectors(cmd.Companions)
	if err != nil {
		return err
	}
	attrs, err := parseStageAttrs(cmd.Attrs)
	if err != nil {
		return err
	}
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, terms)
	if err != nil {
		return err
	}
	body := stageRequestBody{
		Episodes: []stageRequestEpisode{{
			Episode:    episode.String(),
			Media:      mediaSel.String(),
			Source:     cmd.Source,
			Companions: companions,
			Replace:    cmd.Replace,
			Attrs:      attrs,
		}},
	}
	return runStage(rt, c, ref, body, cmd.JSON)
}

func parseStageAttrs(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(raw))
	for _, item := range raw {
		key, value, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("attr %q must be key=value", item)
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("attr %q specified more than once", key)
		}
		out[key] = value
	}
	return out, nil
}

func (cmd *stageTrashCmd) Run(rt *runContext) error {
	terms, trashArg, err := splitStageArgs(cmd.Args)
	if err != nil {
		return err
	}
	pathSel, err := parseStageSeriesArg(trashArg, "path")
	if err != nil {
		return err
	}
	companions, err := parseStageSeriesCompanions(cmd.Companions)
	if err != nil {
		return err
	}
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, terms)
	if err != nil {
		return err
	}
	body := stageRequestBody{
		Trash: []stageRequestTrash{{
			Path:       pathSel.String(),
			Companions: companions,
		}},
	}
	return runStage(rt, c, ref, body, cmd.JSON)
}

func (cmd *stageExtraCmd) Run(rt *runContext) error {
	terms, sourceArg, err := splitStageArgs(cmd.Args)
	if err != nil {
		return err
	}
	sourceSel, err := parseStageSelectorArg(sourceArg, "source")
	if err != nil {
		return err
	}
	c := clientFromRT(rt)
	io := stdio.From(rt.Context)
	ref, err := resolveTermsToRef(rt, c, io, terms)
	if err != nil {
		return err
	}
	body := stageRequestBody{
		Extras: []stageRequestExtra{{
			Season: cmd.Season,
			Source: sourceSel.String(),
			Prefix: cmd.Prefix,
		}},
	}
	return runStage(rt, c, ref, body, cmd.JSON)
}

func runStage(rt *runContext, c *client.Client, ref string, body any, asJSON bool) error {
	ack, err := c.SubmitStage(rt.Context, ref, body)
	if err != nil {
		return err
	}
	raw, err := waitForJobResult(rt, c, ack.JobID)
	if err != nil {
		return err
	}
	var result response.StageResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode stage result: %w", err)
	}
	return render.Stage(rt.Stdout, result, asJSON)
}

// parseStageSelectorArg parses a CLI selector argument and returns a
// helpful error when the operator passes a bare filesystem path instead
// of an inbox: selector. Field is "media" / "source" / "companion" so
// the error points at the right argument.
func parseStageSelectorArg(arg, field string) (selector.Path, error) {
	sel, err := selector.ParseInbox(arg)
	if err != nil {
		return selector.Path{}, fmt.Errorf("%s must be an inbox: selector (e.g. inbox:[BDrip] Show/E01.mkv); use 'kura inbox list' to discover valid selectors: %w", field, err)
	}
	return sel, nil
}

// parseStageMediaArg parses a stage episode media selector. Accepts
// inbox: (normal stage) or series: (in-place metadata override on the
// active record). Other fields (companion, extra source) keep
// inbox-only via parseStageSelectorArg.
func parseStageMediaArg(arg string) (selector.Path, error) {
	sel, err := selector.Parse(arg)
	if err != nil {
		return selector.Path{}, fmt.Errorf("media must be an inbox: or series: selector (e.g. inbox:[BDrip] Show/E01.mkv, or series:Season 1/foo.mkv for in-place metadata override): %w", err)
	}
	if sel.Scheme != selector.Inbox && sel.Scheme != selector.Series {
		return selector.Path{}, fmt.Errorf("media must be an inbox: or series: selector, got %q", sel.Scheme)
	}
	return sel, nil
}

func parseStageCompanionSelectors(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		sel, err := parseStageSelectorArg(raw, "companion")
		if err != nil {
			return nil, err
		}
		out = append(out, sel.String())
	}
	return out, nil
}

// parseStageSeriesArg parses a CLI series: selector argument.
func parseStageSeriesArg(arg, field string) (selector.Path, error) {
	sel, err := selector.ParseSeries(arg)
	if err != nil {
		return selector.Path{}, fmt.Errorf("%s must be a series: selector relative to the series root (e.g. series:Season 1/foo.mkv): %w", field, err)
	}
	return sel, nil
}

func parseStageSeriesCompanions(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		sel, err := parseStageSeriesArg(raw, "companion")
		if err != nil {
			return nil, err
		}
		out = append(out, sel.String())
	}
	return out, nil
}

func splitStageArgs(args []string) (terms []string, path string, err error) {
	if len(args) < 2 {
		return nil, "", errors.New("stage requires at least one selector term and a path")
	}
	return args[:len(args)-1], args[len(args)-1], nil
}

func splitStageEpisodeArgs(args []string) (terms []string, episode, mediaPath string, err error) {
	if len(args) < 3 {
		return nil, "", "", errors.New("stage episode requires at least one selector term, an episode marker, and a media path")
	}
	return args[:len(args)-2], args[len(args)-2], args[len(args)-1], nil
}

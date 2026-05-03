package mcp

import (
	"context"
	"fmt"
	"path/filepath"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/workflow"
)

type showInput struct {
	Ref string `json:"ref" jsonschema:"Metadata ref to inspect (e.g. \"tvdb:370070\"). Get one from kura_resolve."`
}

const showDescription = `Return the observed state of one tracked series: metadata header, every season, every episode with its ` + "`status`" + ` and quality info for active/staged media.

Episode ` + "`status`" + `:
- ` + "`pending`" + `: air date in the future, no media recorded.
- ` + "`missing`" + `: aired, no media recorded.
- ` + "`present`" + `: active media on disk and reachable.
- ` + "`staged`" + `: staged media awaiting reconcile. If ` + "`active`" + ` is also present, reconcile will replace it.
- ` + "`unavailable`" + `: active record exists but file is missing or unreadable.

` + "`active.companions`" + ` are basenames of sidecar files (subtitles, cover art) — useful for upgrade decisions ("does the existing release already have English subs?"). ` + "`staged`" + ` blocks include absolute paths so you can verify your own staging actions.

` + "`inconsistencies`" + ` lists filesystem issues observed at read time, by ` + "`code`" + ` (e.g. ` + "`missing_file`" + `, ` + "`path_escape`" + `); ` + "`reason`" + ` carries the human detail. Operator investigates; an agent should report the existence rather than try to fix.

Trash data is not included.`

// mcpShow is the lean projection of response.Show that this surface
// emits. Fields the agent can't act on (on-disk paths for active
// media, raw series ref, library root) are dropped; staged file paths
// stay absolute so the agent can verify its own staging actions.
type mcpShow struct {
	MetadataRef    string      `json:"metadataRef"`
	PreferredTitle string      `json:"preferredTitle"`
	CanonicalTitle string      `json:"canonicalTitle,omitempty"`
	LastScanned    string      `json:"lastScanned,omitempty"`
	Seasons        []mcpSeason `json:"seasons"`
}

type mcpSeason struct {
	Number   int          `json:"number"`
	Episodes []mcpEpisode `json:"episodes"`
}

type mcpEpisode struct {
	Episode         string             `json:"episode"`
	Aired           string             `json:"aired,omitempty"`
	Status          string             `json:"status"`
	Active          *mcpActiveMedia    `json:"active,omitempty"`
	Staged          *mcpStagedMedia    `json:"staged,omitempty"`
	Inconsistencies []mcpInconsistency `json:"inconsistencies,omitempty"`
}

type mcpActiveMedia struct {
	Source     string   `json:"source"`
	Resolution string   `json:"resolution,omitempty"`
	Codec      string   `json:"codec,omitempty"`
	Size       int64    `json:"size"`
	Companions []string `json:"companions"`
}

type mcpStagedMedia struct {
	Source     string   `json:"source"`
	Resolution string   `json:"resolution,omitempty"`
	Codec      string   `json:"codec,omitempty"`
	Size       int64    `json:"size"`
	File       string   `json:"file"`
	Companions []string `json:"companions"`
}

type mcpInconsistency struct {
	Record string `json:"record"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

func addShowTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_show",
		Title:       "Show series state",
		Description: showDescription,
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in showInput) (*sdkmcp.CallToolResult, any, error) {
		metaRef, err := refs.ParseMetadata(in.Ref)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_show: %v", err),
			}), nil, nil
		}
		seriesRef, ok, err := deps.Workflow.Index.Get(metaRef)
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		if !ok {
			return toolErrorResult(&workflow.MetadataRefNotIndexedError{Ref: metaRef}), nil, nil
		}
		full, err := workflow.Show(ctx, deps.Workflow, workflow.ShowInput{Ref: seriesRef})
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		return nil, projectShow(full), nil
	})
}

// projectShow strips the operator-only fields and collapses
// staged_replacement into staged.
func projectShow(in response.Show) mcpShow {
	out := mcpShow{
		MetadataRef:    in.MetadataRef.String(),
		PreferredTitle: in.PreferredTitle,
		CanonicalTitle: in.CanonicalTitle,
		LastScanned:    in.LastScanned,
		Seasons:        make([]mcpSeason, 0, len(in.Seasons)),
	}
	for _, season := range in.Seasons {
		eps := make([]mcpEpisode, 0, len(season.Episodes))
		for _, ep := range season.Episodes {
			eps = append(eps, projectEpisode(ep))
		}
		out.Seasons = append(out.Seasons, mcpSeason{Number: season.Number, Episodes: eps})
	}
	return out
}

func projectEpisode(ep response.EpisodeShow) mcpEpisode {
	out := mcpEpisode{
		Episode: ep.Episode.String(),
		Aired:   ep.Aired,
		Status:  collapseStatus(ep.Status),
	}
	if ep.Active != nil {
		out.Active = &mcpActiveMedia{
			Source:     ep.Active.Source,
			Resolution: ep.Active.Resolution,
			Codec:      ep.Active.Codec,
			Size:       ep.Active.Size,
			Companions: companionBasenames(ep.Active.Companions),
		}
	}
	if ep.Staged != nil {
		out.Staged = &mcpStagedMedia{
			Source:     ep.Staged.Source,
			Resolution: ep.Staged.Resolution,
			Codec:      ep.Staged.Codec,
			Size:       ep.Staged.Size,
			File:       ep.Staged.File,
			Companions: companionPaths(ep.Staged.Companions),
		}
	}
	if len(ep.Inconsistencies) > 0 {
		out.Inconsistencies = make([]mcpInconsistency, 0, len(ep.Inconsistencies))
		for _, issue := range ep.Inconsistencies {
			out.Inconsistencies = append(out.Inconsistencies, mcpInconsistency{
				Record: issue.Record,
				Code:   issue.Code,
				Reason: issue.Reason,
			})
		}
	}
	return out
}

// collapseStatus maps staged_replacement → staged. The replacement
// nuance is encoded by `active` being present alongside `staged`.
func collapseStatus(s response.Status) string {
	if s == response.StatusStagedReplacement {
		return string(response.StatusStaged)
	}
	return string(s)
}

func companionBasenames(in []response.CompanionShow) []string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		out = append(out, filepath.Base(c.Path))
	}
	return out
}

func companionPaths(in []response.CompanionShow) []string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		out = append(out, c.Path)
	}
	return out
}

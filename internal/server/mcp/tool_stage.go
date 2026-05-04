package mcp

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/workflow"
)

type stageInput struct {
	Ref            string   `json:"ref" jsonschema:"Metadata ref (e.g. \"tvdb:370070\") from kura_resolve."`
	Episode        string   `json:"episode" jsonschema:"Episode marker (S01E03) or storage form (S01E0003)."`
	MediaPath      string   `json:"mediaPath" jsonschema:"Path to the media file. Absolute path, or series-root-relative slash form (e.g. \"Season 2/foo.mkv\") — the same form scan/show output uses."`
	Source         string   `json:"source,omitempty" jsonschema:"Override for the source label. One of: BluRay, WebRip, Web-DL, HDTV, DVDRip, TVRip, Unknown. Otherwise inferred from the filename."`
	CompanionPaths []string `json:"companionPaths,omitempty" jsonschema:"Sidecar paths (subtitles, cover art) to attach to the stage. Absolute or series-root-relative, same conventions as mediaPath."`
	Replace        bool     `json:"replace,omitempty" jsonschema:"Allow staging over an existing active or staged record at this slot."`
}

const stageDescription = `Stage a media file for one episode slot. Records intent only — the file stays in place until ` + "`kura_reconcile_apply`" + ` runs and moves it into the series's canonical layout, archiving any prior file at that slot to trash.

` + "`mediaPath`" + ` accepts either an absolute path on the server's filesystem or a series-root-relative slash path (e.g. ` + "`\"Season 2/foo.mkv\"`" + `) — the latter is the same form ` + "`kura_scan`" + ` and ` + "`kura_show`" + ` emit, so paths from those tools can be passed back verbatim. ` + "`companionPaths`" + ` carries sidecar files (subtitles, cover art) under the same conventions.

` + "`source`" + ` overrides what kura would otherwise infer from the filename (BluRay, WebRip, Web-DL, HDTV, DVDRip, TVRip, Unknown). Resolution / codec / size are always probed from the file.

Refuses if the slot already has an active or staged record unless ` + "`replace`" + ` is true.`

func addStageTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_stage",
		Title:       "Stage media for episode",
		Description: stageDescription,
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    false,
			IdempotentHint:  false,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in stageInput) (*sdkmcp.CallToolResult, any, error) {
		metaRef, err := refs.ParseMetadata(in.Ref)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_stage: %v", err),
			}), nil, nil
		}
		episode, err := refs.ParseEpisodeMarker(in.Episode)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_stage: episode: %v", err),
			}), nil, nil
		}
		seriesRef, ok, err := deps.Workflow.Index.Get(metaRef)
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		if !ok {
			return toolErrorResult(&workflow.MetadataRefNotIndexedError{Ref: metaRef}), nil, nil
		}
		if _, err := workflow.Stage(ctx, deps.Workflow, workflow.StageInput{
			Ref:            seriesRef,
			Episode:        episode,
			MediaPath:      in.MediaPath,
			Source:         in.Source,
			CompanionPaths: in.CompanionPaths,
			Replace:        in.Replace,
		}); err != nil {
			return toolErrorResult(err), nil, nil
		}
		return nil, nil, nil
	})
}

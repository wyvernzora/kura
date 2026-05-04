package mcp

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/workflow"
)

type trashInput struct {
	Ref  string `json:"ref" jsonschema:"Metadata ref (e.g. \"tvdb:370070\") from kura_resolve."`
	Path string `json:"path" jsonschema:"Path to the media file to trash. Absolute or series-root-relative slash form (same conventions as kura_stage and kura_scan output, e.g. \"Season 2/foo.mkv\")."`
}

const trashDescription = `Move one media file (and its filename-matched companion sidecars) from the series directory into trash. Recoverable later via the CLI ` + "`kura trash restore`" + `.

Use cases:
- Trashing the loser of a duplicate-slot pair after staging the winner.
- Removing a stray non-canonical file the operator wants gone but recoverable.

` + "`path`" + ` accepts either an absolute path on the server's filesystem or a series-root-relative slash path — pass back ` + "`kura_scan`" + ` skip rows verbatim. Companions sharing the media basename in the same directory are auto-discovered and travel with the media.

Refuses when:
- The file is the active or staged record for an episode in the series. Use ` + "`kura_stage`" + ` with ` + "`replace`" + ` (active) or ` + "`kura_reset`" + ` (staged) to clear the record first.
- The filename does not parse to a (season, episode) slot — orphan files require manual cleanup.
- The file does not exist or lives outside the series root.

Returns the new trash entry's ` + "`id`" + ` (ULID), inferred ` + "`episode`" + `, and the relative paths of the moved media + companions.`

func addTrashTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_trash",
		Title:       "Trash one media file",
		Description: trashDescription,
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    false,
			IdempotentHint:  false,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintTrue,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in trashInput) (*sdkmcp.CallToolResult, any, error) {
		metaRef, err := refs.ParseMetadata(in.Ref)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_trash: %v", err),
			}), nil, nil
		}
		seriesRef, ok, err := deps.Workflow.Index.Get(metaRef)
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		if !ok {
			return toolErrorResult(&workflow.MetadataRefNotIndexedError{Ref: metaRef}), nil, nil
		}
		result, err := workflow.TrashAdd(ctx, deps.Workflow, workflow.TrashAddInput{
			Ref:  seriesRef,
			Path: in.Path,
		})
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		return nil, result, nil
	})
}

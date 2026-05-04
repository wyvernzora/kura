package mcp

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/workflow"
)

type importInput struct {
	Ref     string `json:"ref" jsonschema:"Metadata ref (e.g. \"tvdb:370070\") from kura_resolve."`
	Dirname string `json:"dirname" jsonschema:"Existing directory basename under the library root to adopt."`
}

const importDescription = `Adopt an existing directory under the library root as a tracked series at the given metadata ref. Companion to ` + "`kura_add`" + `, which creates a new directory; use ` + "`kura_import`" + ` when the directory already exists (operator dropped files in, or ` + "`kura_list`" + ` returned an ` + "`untracked`" + ` row).

Refuses if the directory is already tracked or doesn't exist on disk. To re-track a directory whose ` + "`.kura/series.json`" + ` is corrupted, an operator must use the CLI ` + "`--force`" + ` form; agents can't bypass.`

func addImportTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_import",
		Title:       "Import existing directory as series",
		Description: importDescription,
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    false,
			IdempotentHint:  false,
			OpenWorldHint:   &hintTrue,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in importInput) (*sdkmcp.CallToolResult, any, error) {
		metaRef, err := refs.ParseMetadata(in.Ref)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_import: %v", err),
			}), nil, nil
		}
		if in.Dirname == "" {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: "kura_import: dirname is required",
			}), nil, nil
		}
		seriesRef, err := refs.ParseSeries(in.Dirname)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_import: dirname: %v", err),
			}), nil, nil
		}
		if _, err := workflow.Import(ctx, deps.Workflow, workflow.ImportInput{
			Metadata: metaRef,
			Ref:      seriesRef,
		}); err != nil {
			return toolErrorResult(err), nil, nil
		}
		return nil, ackSuccess, nil
	})
}

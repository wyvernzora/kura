package mcp

import (
	"context"
	_ "embed"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider/tvdb"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

type importInput struct {
	Ref      string `json:"ref" jsonschema:"Metadata ref, e.g. \"tvdb:370070\"."`
	Dirname  string `json:"dirname" jsonschema:"Existing directory basename under the library root to adopt."`
	Ordering string `json:"ordering,omitempty" jsonschema:"Pin the per-series episode ordering used for the initial spine fetch. One of: default, official, dvd, absolute, alternate, regional. Omit to use the provider's default."`
}

//go:embed tool_import.md
var toolImportDoc string

func addImportTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_import",
		Title:       "Import existing directory as series",
		Description: forLLM(toolImportDoc),
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
		ordering, err := tvdb.ParseOrdering(in.Ordering)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_import: %v", err),
			}), nil, nil
		}
		result, err := workflow.Import(ctx, deps.Workflow, workflow.ImportInput{
			Metadata: metaRef,
			Ref:      seriesRef,
			Ordering: ordering,
		})
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		return nil, result, nil
	})
}

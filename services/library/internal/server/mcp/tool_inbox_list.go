package mcp

import (
	"context"
	_ "embed"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/workflow"
)

// inboxListInput mirrors workflow.InboxListInput at the wire boundary.
// Field names use camelCase to match other MCP tool inputs.
type inboxListInput struct {
	Path          string `json:"path,omitempty" jsonschema:"Optional path relative to the inbox root (forward-slash, no leading slash). A directory lists its children; a file returns that exact entry. Empty lists the root."`
	Recursive     bool   `json:"recursive,omitempty" jsonschema:"When true, walks subdirectories up to depth levels deep."`
	Depth         int    `json:"depth,omitempty" jsonschema:"Recursive depth cap (default 3, max 5). Ignored when recursive=false."`
	Limit         int    `json:"limit,omitempty" jsonschema:"Cap on entries returned (default 500, max 5000). Truncation surfaces in the trailing footer."`
	Kind          string `json:"kind,omitempty" jsonschema:"Optional kind filter: 'file', 'dir', or 'symlink'."`
	NameGlob      string `json:"nameGlob,omitempty" jsonschema:"Optional filepath.Match-style basename glob (e.g. '*.mkv')."`
	IncludeHidden bool   `json:"includeHidden,omitempty" jsonschema:"When true, surfaces dotfiles and download-in-flight markers (.partial, .crdownload, etc.)."`
}

//go:embed tool_inbox_list.md
var toolInboxListDoc string

func addInboxListTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_inbox_list",
		Title:       "List inbox contents",
		Description: forLLM(toolInboxListDoc),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in inboxListInput) (*sdkmcp.CallToolResult, any, error) {
		if in.Kind != "" && in.Kind != "file" && in.Kind != "dir" && in.Kind != "symlink" {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: "kura_inbox_list: kind must be 'file', 'dir', or 'symlink' (or empty for any)",
			}), nil, nil
		}
		result, err := workflow.InboxList(ctx, deps.Workflow, workflow.InboxListInput{
			Path:          in.Path,
			Recursive:     in.Recursive,
			Depth:         in.Depth,
			Limit:         in.Limit,
			Kind:          in.Kind,
			NameGlob:      in.NameGlob,
			IncludeHidden: in.IncludeHidden,
		})
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		return &sdkmcp.CallToolResult{StructuredContent: result}, nil, nil
	})
}

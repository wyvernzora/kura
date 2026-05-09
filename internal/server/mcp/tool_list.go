package mcp

import (
	"context"
	_ "embed"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/workflow"
)

type listInput struct {
	Statuses   []string `json:"statuses,omitempty" jsonschema:"Optional status filter. Allowed values: complete, incomplete, error, untracked. Empty/omitted returns all four."`
	Airing     *bool    `json:"airing,omitempty" jsonschema:"Optional airing-flag filter. true admits only currently-airing series (first episode aired or airs within 168h, with at least one future episode); false admits non-airing only. Omit for no filter."`
	MaxResults int      `json:"maxResults,omitempty" jsonschema:"Maximum rows per response. 0 or omitted uses the server default (100). Values above 1000 are clamped."`
	Cursor     string   `json:"cursor,omitempty" jsonschema:"Opaque pagination token from a previous response's nextCursor. Omit for the first page."`
}

const (
	defaultListMaxResults = 100
	maxListMaxResults     = 1000
)

//go:embed tool_list.md
var toolListDoc string

var allowedListStatuses = map[response.ListStatus]struct{}{
	response.ListStatusComplete:   {},
	response.ListStatusIncomplete: {},
	response.ListStatusError:      {},
	response.ListStatusUntracked:  {},
}

func addListTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_list",
		Title:       "List tracked series",
		Description: forLLM(toolListDoc),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in listInput) (*sdkmcp.CallToolResult, any, error) {
		statuses, err := parseListStatuses(in.Statuses)
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		max := in.MaxResults
		switch {
		case max < 0:
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: "kura_list: maxResults must be >= 0",
			}), nil, nil
		case max == 0:
			max = defaultListMaxResults
		case max > maxListMaxResults:
			max = maxListMaxResults
		}
		result, err := workflow.List(ctx, deps.Workflow, workflow.ListInput{
			Statuses:   statuses,
			Airing:     in.Airing,
			MaxResults: max,
			Cursor:     in.Cursor,
		})
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		return nil, result, nil
	})
}

// parseListStatuses validates each entry is in the closed allowed set
// and returns the typed slice. An unknown value yields an
// invalidInputError with kind=invalid_ref.
func parseListStatuses(raw []string) ([]response.ListStatus, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]response.ListStatus, 0, len(raw))
	for _, s := range raw {
		status := response.ListStatus(s)
		if _, ok := allowedListStatuses[status]; !ok {
			return nil, &invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_list: unknown status %q (allowed: complete, incomplete, error, untracked)", s),
			}
		}
		out = append(out, status)
	}
	return out, nil
}

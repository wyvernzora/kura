package mcp

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/workflow"
)

type listInput struct {
	Statuses []string `json:"statuses,omitempty" jsonschema:"Optional status filter. Allowed values: complete, incomplete, airing, error, untracked. Empty/omitted returns all five."`
}

const listDescription = `List series under the library root with summary state per row (status, episode counts, last scan time).

Filter with ` + "`statuses`" + `. Empty/omitted = all five.

Status meanings:
- ` + "`complete`" + `: every aired episode has present media.
- ` + "`incomplete`" + `: at least one aired episode is missing.
- ` + "`airing`" + `: every aired episode is present, more episodes upcoming.
- ` + "`error`" + `: row could not be computed; ` + "`error`" + ` field carries the message.
- ` + "`untracked`" + `: directory exists under the library root but has no .kura/series.json (kura does not manage it).

The ` + "`staged`" + ` flag is independent of status — true if any episode has staged media awaiting reconcile.`

var allowedListStatuses = map[response.ListStatus]struct{}{
	response.ListStatusComplete:   {},
	response.ListStatusIncomplete: {},
	response.ListStatusAiring:     {},
	response.ListStatusError:      {},
	response.ListStatusUntracked:  {},
}

func addListTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_list",
		Title:       "List tracked series",
		Description: listDescription,
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
		result, err := workflow.List(ctx, deps.Workflow, workflow.ListInput{Statuses: statuses})
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
				message: fmt.Sprintf("kura_list: unknown status %q (allowed: complete, incomplete, airing, error, untracked)", s),
			}
		}
		out = append(out, status)
	}
	return out, nil
}

package mcp

import (
	"context"
	"errors"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/errkind"
)

// toolErrorResult turns any error into a CallToolResult with
// IsError=true and a structured payload describing kind, category,
// and any error-specific data. Coded errors expose taxonomy via
// errkind.Coded; unknown errors fall back to a generic internal
// payload.
//
// Tool handlers should call this and return the result with a nil
// Go error rather than letting the SDK wrap a bare error; the
// structured payload is otherwise lost.
func toolErrorResult(err error) *sdkmcp.CallToolResult {
	res := &sdkmcp.CallToolResult{}
	res.SetError(err)
	res.StructuredContent = errorPayload(err)
	return res
}

// errorPayload builds the StructuredContent body for a tool error.
// Always includes kind, category, and message. Coded errors merge
// their Data() map; collisions with the reserved fields above are
// overwritten so the taxonomy stays deterministic.
func errorPayload(err error) map[string]any {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return map[string]any{
			"kind":     errkind.KindInternal,
			"category": errkind.CategoryCancelled,
			"message":  err.Error(),
		}
	}
	if coded, ok := errors.AsType[errkind.Coded](err); ok {
		out := coded.Data()
		if out == nil {
			out = map[string]any{}
		}
		out["kind"] = coded.Kind()
		out["category"] = coded.Category()
		out["message"] = err.Error()
		return out
	}
	return map[string]any{
		"kind":     errkind.KindInternal,
		"category": errkind.CategoryInternalError,
		"message":  err.Error(),
	}
}

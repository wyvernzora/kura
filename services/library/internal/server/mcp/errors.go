package mcp

import (
	"context"
	"errors"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library/internal/errkind"
)

// invalidInputError is the generic input-validation error tool
// handlers raise when the SDK schema can't express the constraint
// (e.g. minItems on a slice, mutually-exclusive fields). It
// implements errkind.Coded so the standard mapper renders it.
type invalidInputError struct {
	kind    string
	message string
}

func (e *invalidInputError) Error() string        { return e.message }
func (e *invalidInputError) Kind() string         { return e.kind }
func (e *invalidInputError) Category() string     { return errkind.CategoryInvalidParams }
func (e *invalidInputError) Data() map[string]any { return map[string]any{} }

// errEmptyTerms is raised by kura_resolve when the input array is
// present but empty. The schema marks `terms` required but cannot
// express minItems on a slice.
var errEmptyTerms = &invalidInputError{
	kind:    errkind.KindInvalidRef,
	message: "kura_resolve: terms must contain at least one entry",
}

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

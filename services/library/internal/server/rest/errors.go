package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/wyvernzora/kura/services/library/internal/errkind"
)

// maxInternalMessage caps the message length for non-coded errors so
// arbitrary upstream errors can't grow the response unbounded.
const maxInternalMessage = 512

// errorEnvelope is the wire shape for every non-2xx response. Mirrors
// the MCP error data shape: kind + category + message, optional data.
type errorEnvelope struct {
	Kind     string         `json:"kind"`
	Category string         `json:"category"`
	Message  string         `json:"message"`
	Data     map[string]any `json:"data,omitempty"`
}

// writeError encodes err as a JSON error envelope. The HTTP status is
// derived from the error chain via encodeError.
func writeError(w http.ResponseWriter, err error) {
	status, env := encodeError(err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set(headerCacheControl, cacheControlNoStore)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

// encodeError walks the error chain. errkind.Coded errors map by Kind
// + Category. REST-local sentinel types (validationError, forbiddenError,
// internalError) map directly. Anything else collapses to a 500
// internal envelope with truncated message.
func encodeError(err error) (int, errorEnvelope) {
	if coded, ok := errors.AsType[errkind.Coded](err); ok {
		return statusForKind(coded.Kind(), coded.Category()), errorEnvelope{
			Kind:     coded.Kind(),
			Category: coded.Category(),
			Message:  coded.Error(),
			Data:     coded.Data(),
		}
	}
	if ve, ok := errors.AsType[*validationError](err); ok {
		return http.StatusBadRequest, errorEnvelope{
			Kind:     errkind.KindInvalidRef,
			Category: errkind.CategoryInvalidParams,
			Message:  ve.Error(),
		}
	}
	if fe, ok := errors.AsType[*forbiddenError](err); ok {
		return http.StatusForbidden, errorEnvelope{
			Kind:     "forbidden",
			Category: errkind.CategoryInvalidParams,
			Message:  fe.Error(),
		}
	}
	if ue, ok := errors.AsType[*unauthorizedError](err); ok {
		return http.StatusUnauthorized, errorEnvelope{
			Kind:     "unauthorized",
			Category: errkind.CategoryInvalidParams,
			Message:  ue.Error(),
		}
	}
	// Common storage-layer errors that should surface as not-found.
	// Workflows that propagate os.ErrNotExist verbatim land here.
	// Don't echo err.Error() — the underlying message includes the
	// server's on-disk libRoot path, which is meaningless / leaky to
	// host-side callers (the path is the *container's* libRoot for
	// dockerized deploys).
	if errors.Is(err, os.ErrNotExist) {
		return http.StatusNotFound, errorEnvelope{
			Kind:     errkind.KindNotFound,
			Category: errkind.CategoryInvalidParams,
			Message:  "resource not found",
		}
	}
	msg := err.Error()
	if len(msg) > maxInternalMessage {
		msg = msg[:maxInternalMessage]
	}
	return http.StatusInternalServerError, errorEnvelope{
		Kind:     errkind.KindInternal,
		Category: errkind.CategoryInternalError,
		Message:  msg,
	}
}

// statusForKind picks an HTTP status from a workflow Kind. Falls back
// to category when Kind is unmapped, and finally to 500.
func statusForKind(kind, category string) int {
	switch kind {
	case errkind.KindNotFound:
		return http.StatusNotFound
	case errkind.KindConflict,
		errkind.KindBusy,
		errkind.KindPlanApplied,
		errkind.KindStaleSnapshot:
		return http.StatusConflict
	case errkind.KindInvalidEpisode,
		errkind.KindInvalidTag,
		errkind.KindNoStaged,
		errkind.KindUnsupportedProvider,
		errkind.KindInvalidCursor,
		errkind.KindBatchTooLarge:
		return http.StatusUnprocessableEntity
	case errkind.KindServerNotReady:
		return http.StatusServiceUnavailable
	case errkind.KindClaimStolen, errkind.KindApplyStepFailed:
		return http.StatusInternalServerError
	case errkind.KindInvalidRef:
		return http.StatusBadRequest
	case errkind.KindProviderUnavailable:
		return http.StatusBadGateway
	}
	if category == errkind.CategoryInvalidParams {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// internalError is the generic non-coded error type. Used by the
// recover middleware and any handler that needs to surface a sanitized
// message without leaking internals.
type internalError struct {
	msg string
}

func (e *internalError) Error() string { return e.msg }

// forbiddenError is the operator-gate failure error.
type forbiddenError struct {
	msg string
}

func (e *forbiddenError) Error() string { return e.msg }

// validationError is for request-shape errors raised inside REST
// handlers (bad JSON, missing required field, malformed query param).
// Maps to 400 with kind=invalid_ref.
type validationError struct {
	msg string
}

func (e *validationError) Error() string { return e.msg }

// unauthorizedError is the missing-or-invalid-bearer-token error.
// Maps to 401. Used by bearerAuthMiddleware.
type unauthorizedError struct {
	msg string
}

func (e *unauthorizedError) Error() string { return e.msg }

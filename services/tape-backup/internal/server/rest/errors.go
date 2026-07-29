package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/wyvernzora/kura/services/tape-backup/internal/service"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
)

const maxInternalMessage = 512

type errorEnvelope struct {
	Kind     string `json:"kind"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

type validationError struct {
	message string
}

func (e *validationError) Error() string {
	return e.message
}

type internalError struct {
	message string
}

func (e *internalError) Error() string {
	return e.message
}

func writeError(w http.ResponseWriter, err error) {
	status, envelope := encodeError(err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope)
}

func encodeError(err error) (int, errorEnvelope) {
	if refusal, ok := errors.AsType[*service.Refusal](err); ok {
		return http.StatusConflict, errorEnvelope{
			Kind:     refusal.Code,
			Category: "invalid_params",
			Message:  refusal.Message,
		}
	}
	if validation, ok := errors.AsType[*validationError](err); ok {
		return http.StatusBadRequest, errorEnvelope{
			Kind:     "invalid_ref",
			Category: "invalid_params",
			Message:  validation.Error(),
		}
	}
	if internal, ok := errors.AsType[*internalError](err); ok {
		return http.StatusInternalServerError, errorEnvelope{
			Kind:     "internal",
			Category: "internal_error",
			Message:  internal.Error(),
		}
	}
	switch {
	case errors.Is(err, service.ErrSessionActive):
		return conflictEnvelope("busy", "session is already active")
	case errors.Is(err, service.ErrApprovalRequired):
		return conflictEnvelope("approval_required", "init plan is awaiting approval")
	case errors.Is(err, service.ErrNoWork):
		return conflictEnvelope("no_work", "no pending snapshots for loaded tape")
	case errors.Is(err, service.ErrApprovalNotAllowed):
		return http.StatusConflict, errorEnvelope{
			Kind:     "approval_not_allowed",
			Category: "invalid_params",
			Message:  "only init drafts can be approved",
		}
	case errors.Is(err, backupplan.ErrInvalidPlanID):
		return http.StatusBadRequest, errorEnvelope{
			Kind:     "invalid_ref",
			Category: "invalid_params",
			Message:  "plan ID must be a canonical ULID",
		}
	case errors.Is(err, backupplan.ErrPlanDone):
		return http.StatusConflict, errorEnvelope{
			Kind:     "plan_done",
			Category: "invalid_params",
			Message:  "done plan cannot be discarded",
		}
	case errors.Is(err, backupplan.ErrPlanDiscarded):
		return http.StatusConflict, errorEnvelope{
			Kind:     "plan_discarded",
			Category: "invalid_params",
			Message:  "plan is already discarded",
		}
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound, errorEnvelope{
			Kind:     "not_found",
			Category: "invalid_params",
			Message:  "resource not found",
		}
	}
	message := err.Error()
	if len(message) > maxInternalMessage {
		message = message[:maxInternalMessage]
	}
	return http.StatusInternalServerError, errorEnvelope{
		Kind:     "internal",
		Category: "internal_error",
		Message:  message,
	}
}

func conflictEnvelope(kind string, message string) (int, errorEnvelope) {
	return http.StatusConflict, errorEnvelope{
		Kind:     kind,
		Category: "invalid_params",
		Message:  message,
	}
}

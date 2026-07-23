package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

func mustSeries(t *testing.T, name string) refs.Series {
	t.Helper()
	r, err := refs.ParseSeries(name)
	if err != nil {
		t.Fatalf("ParseSeries(%q): %v", name, err)
	}
	return r
}

func TestErrorPayload_PlainErrorIsInternal(t *testing.T) {
	got := errorPayload(errors.New("boom"))
	if got["kind"] != errkind.KindInternal {
		t.Fatalf("kind = %v, want %v", got["kind"], errkind.KindInternal)
	}
	if got["category"] != errkind.CategoryInternalError {
		t.Fatalf("category = %v, want %v", got["category"], errkind.CategoryInternalError)
	}
	if got["message"] != "boom" {
		t.Fatalf("message = %v, want %q", got["message"], "boom")
	}
}

func TestErrorPayload_ContextCanceledIsCancelled(t *testing.T) {
	got := errorPayload(context.Canceled)
	if got["category"] != errkind.CategoryCancelled {
		t.Fatalf("category = %v, want %v", got["category"], errkind.CategoryCancelled)
	}
}

func TestErrorPayload_WorkflowNotFoundIsCoded(t *testing.T) {
	ref := mustSeries(t, "Bookworm")
	got := errorPayload(&workflow.NotFoundError{Ref: ref})
	if got["kind"] != errkind.KindNotFound {
		t.Fatalf("kind = %v, want %v", got["kind"], errkind.KindNotFound)
	}
	if got["category"] != errkind.CategoryInvalidParams {
		t.Fatalf("category = %v, want %v", got["category"], errkind.CategoryInvalidParams)
	}
	if got["ref"] != ref.String() {
		t.Fatalf("ref = %v, want %q", got["ref"], ref.String())
	}
}

func TestErrorPayload_CoordBusyHasHolder(t *testing.T) {
	got := errorPayload(&coord.BusyError{
		Scope:  "library",
		Holder: coord.Holder{Op: "scan", PID: 42, Host: "h"},
	})
	if got["kind"] != errkind.KindBusy {
		t.Fatalf("kind = %v, want %v", got["kind"], errkind.KindBusy)
	}
	holder, ok := got["holder"].(map[string]any)
	if !ok {
		t.Fatalf("holder = %v, want map", got["holder"])
	}
	if holder["op"] != "scan" || holder["pid"] != 42 {
		t.Fatalf("holder = %+v", holder)
	}
}

func TestErrorPayload_JobNotFoundIsCoded(t *testing.T) {
	got := errorPayload(&jobs.JobNotFoundError{JobID: "abc"})
	if got["kind"] != errkind.KindNotFound {
		t.Fatalf("kind = %v, want %v", got["kind"], errkind.KindNotFound)
	}
	if got["jobId"] != "abc" {
		t.Fatalf("jobId = %v, want %q", got["jobId"], "abc")
	}
}

func TestToolErrorResult_SetsIsErrorAndStructuredContent(t *testing.T) {
	res := toolErrorResult(&workflow.NotFoundError{Ref: mustSeries(t, "X")})
	if !res.IsError {
		t.Fatal("IsError = false, want true")
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent = %T, want map[string]any", res.StructuredContent)
	}
	if body["kind"] != errkind.KindNotFound {
		t.Fatalf("kind = %v, want %v", body["kind"], errkind.KindNotFound)
	}
}

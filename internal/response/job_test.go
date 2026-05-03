package response_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/response"
)

func mustSeries(t *testing.T, s string) refs.Series {
	t.Helper()
	out, err := refs.ParseSeries(s)
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	return out
}

func TestJobHandle_JSONShape(t *testing.T) {
	started := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	h := response.JobHandle{
		JobID:     "abcdef0123456789",
		Kind:      "scan",
		Series:    mustSeries(t, "show-x"),
		StartedAt: started,
	}
	b, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{"jobId", "kind", "series", "startedAt"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q in %s", key, b)
		}
	}
	if got["jobId"] != "abcdef0123456789" {
		t.Errorf("jobId = %v", got["jobId"])
	}
}

func TestJobStatus_OmitsOptionalFields(t *testing.T) {
	started := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	s := response.JobStatus{
		JobID:     "abcdef0123456789",
		Kind:      "scan",
		Series:    mustSeries(t, "show-x"),
		State:     "running",
		StartedAt: started,
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{"endedAt", "progress", "result", "error"} {
		if _, ok := got[key]; ok {
			t.Errorf("expected key %q omitted on running job; got %s", key, b)
		}
	}
}

func TestJobError_DataPayloadRoundtrips(t *testing.T) {
	e := response.JobError{
		Kind:    "busy",
		Message: "another job in flight",
		Data: map[string]any{
			"existingJob": map[string]any{
				"jobId": "deadbeefdeadbeef",
				"kind":  "reconcile_apply",
			},
		},
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back response.JobError
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.Kind != "busy" {
		t.Errorf("Kind = %q", back.Kind)
	}
	inner, _ := back.Data["existingJob"].(map[string]any)
	if inner["jobId"] != "deadbeefdeadbeef" {
		t.Errorf("data.existingJob.jobId = %v", inner["jobId"])
	}
}

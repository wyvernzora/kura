package workflow_test

import (
	"context"
	"testing"

	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/workflow"
)

// TestApplyReconcile_AlwaysReturnsTrackedJob is the per-workflow
// IsTracked invariant test. Long workflows MUST always go through
// jobs.Submit; the MCP long-tool handler relies on this to construct
// a JobHandle unconditionally per design/async-job.md § 11.10.
func TestApplyReconcile_AlwaysReturnsTrackedJob(t *testing.T) {
	deps, ref := seedSeries(t)
	deps.Jobs = jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { deps.Jobs.Shutdown(0) })

	j := workflow.ApplyReconcile(context.Background(), deps, workflow.ApplyReconcileInput{Ref: ref, Token: "deadbeefdead"})
	if !j.IsTracked() {
		t.Fatalf("ApplyReconcile must always return a tracked job; got IsTracked=false")
	}
	if j.ID() == "" {
		t.Fatalf("tracked job must have non-empty ID")
	}
	if j.Kind() != string(jobs.KindReconcileApply) {
		t.Fatalf("Job.Kind = %q, want %q", j.Kind(), jobs.KindReconcileApply)
	}
	// Don't Wait — closure will fail looking up the bogus token, but
	// that's a goroutine concern. Invariant is about Submit being
	// called regardless of input validity.
}

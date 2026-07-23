package workflow_test

import (
	"context"
	"testing"

	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

// TestScan_AlwaysReturnsTrackedJob is the per-workflow IsTracked
// invariant test. Long workflows MUST always go through jobs.Submit;
// any code path that returns a pre-resolved Job from Scan is a bug
// per design/async-job.md § 11.10. The MCP long-tool handler relies
// on this invariant to construct a JobHandle unconditionally.
func TestScan_AlwaysReturnsTrackedJob(t *testing.T) {
	deps, ref := seedSeries(t)
	deps.Jobs = jobs.NewRegistry(context.Background(), jobs.Config{}, nil)
	t.Cleanup(func() { deps.Jobs.Shutdown(0) })

	j := workflow.Scan(context.Background(), deps, workflow.ScanInput{Ref: ref})
	if !j.IsTracked() {
		t.Fatalf("Scan must always return a tracked job; got IsTracked=false")
	}
	if j.ID() == "" {
		t.Fatalf("tracked job must have non-empty ID")
	}
	if j.Kind() != string(jobs.KindScan) {
		t.Fatalf("Job.Kind = %q, want %q", j.Kind(), jobs.KindScan)
	}
	// Don't Wait — fn would call deps.Provider() which is nil. The
	// invariant is about Submit being called regardless of input
	// validity.
}

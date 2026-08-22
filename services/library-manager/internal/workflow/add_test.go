package workflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

// TestAddSeedsMaintenanceRequestedTag guards the registration default:
// a newly added series carries maintenance:requested so the maintenance
// agent picks it up on its next pass.
func TestAddSeedsMaintenanceRequestedTag(t *testing.T) {
	ref, err := refs.ParseSeries("Show A")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	root := t.TempDir()
	deps := workflow.Deps{
		LibRoot:     root,
		Index:       indexfile.New(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()}),
		Coordinator: coord.NewMCPCoordinator(),
		Now:         time.Now,
		Provider:    func() (provider.Source, error) { return stubSource{}, nil },
	}

	if _, err := workflow.Add(context.Background(), deps, workflow.AddInput{
		Metadata: refs.Metadata("stub:42"),
		Ref:      ref,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	model, err := seriesfile.Load(root, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(model.Tags) != 1 || model.Tags[0] != "maintenance:requested" {
		t.Fatalf("Tags = %v, want [maintenance:requested]", model.Tags)
	}
}

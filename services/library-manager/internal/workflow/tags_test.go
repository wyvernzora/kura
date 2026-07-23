package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

func TestUpdateTagsAddsRemovesAndNoOps(t *testing.T) {
	deps, ref := seedSeries(t)

	first, err := workflow.UpdateTags(context.Background(), deps, workflow.UpdateTagsInput{
		Ref: ref, Tags: []string{"Priority", "Maintenance-Disabled"},
	})
	if err != nil {
		t.Fatalf("first UpdateTags: %v", err)
	}
	if len(first.Tags) != 2 || first.Tags[0] != "maintenance-disabled" || first.Tags[1] != "priority" {
		t.Fatalf("first tags = %v", first.Tags)
	}

	second, err := workflow.UpdateTags(context.Background(), deps, workflow.UpdateTagsInput{
		Ref: ref, Tags: []string{"!MAINTENANCE-DISABLED", "PRIORITY"},
	})
	if err != nil {
		t.Fatalf("second UpdateTags: %v", err)
	}
	if len(second.Tags) != 1 || second.Tags[0] != "priority" {
		t.Fatalf("second tags = %v", second.Tags)
	}

	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	hash := model.Hash
	if _, err := workflow.UpdateTags(context.Background(), deps, workflow.UpdateTagsInput{
		Ref: ref, Tags: []string{"priority", "!missing"},
	}); err != nil {
		t.Fatalf("no-op UpdateTags: %v", err)
	}
	reloaded, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Hash != hash {
		t.Fatalf("no-op hash changed: got %s want %s", reloaded.Hash, hash)
	}
}

func TestUpdateTagsRemovalOnlyReturnsEmptyJSONArray(t *testing.T) {
	deps, ref := seedSeries(t)

	out, err := workflow.UpdateTags(context.Background(), deps, workflow.UpdateTagsInput{
		Ref: ref, Tags: []string{"!Missing"},
	})
	if err != nil {
		t.Fatalf("UpdateTags: %v", err)
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got, want := string(data), `{"metadataRef":"tvdb:42","tags":[]}`; got != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}

func TestImportForcePreservesTags(t *testing.T) {
	deps, ref := seedSeries(t)
	if _, err := workflow.UpdateTags(context.Background(), deps, workflow.UpdateTagsInput{
		Ref: ref, Tags: []string{"Priority"},
	}); err != nil {
		t.Fatalf("UpdateTags: %v", err)
	}
	deps.Index = indexfile.New(deps.LibRoot, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	deps.Provider = func() (provider.Source, error) { return stubSource{}, nil }

	if _, err := workflow.Import(context.Background(), deps, workflow.ImportInput{
		Metadata: refs.Metadata("stub:42"),
		Ref:      ref,
		Force:    true,
	}); err != nil {
		t.Fatalf("Import force: %v", err)
	}
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(model.Tags) != 1 || model.Tags[0] != "priority" {
		t.Fatalf("Tags = %v, want [priority]", model.Tags)
	}
}

func TestUpdateTagsRejectsInvalidExpressions(t *testing.T) {
	deps, ref := seedSeries(t)
	tests := []struct {
		name string
		tags []string
	}{
		{name: "empty"},
		{name: "invalid syntax", tags: []string{"high priority"}},
		{name: "conflict", tags: []string{"priority", "!priority"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := workflow.UpdateTags(context.Background(), deps, workflow.UpdateTagsInput{Ref: ref, Tags: tt.tags})
			var invalid *workflow.InvalidTagError
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %T %v, want InvalidTagError", err, err)
			}
		})
	}
}

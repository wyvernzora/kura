package workflow_test

import (
	"errors"
	"testing"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/workflow"
)

func TestProviderFactoryCachesSuccess(t *testing.T) {
	calls := 0
	factory := workflow.NewProviderFactory(func() (metadata.Source, error) {
		calls++
		return nil, nil
	})
	for i := 0; i < 3; i++ {
		if _, err := factory(); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if calls != 1 {
		t.Fatalf("construct calls = %d, want 1", calls)
	}
}

func TestProviderFactoryCachesFailure(t *testing.T) {
	calls := 0
	want := errors.New("missing key")
	factory := workflow.NewProviderFactory(func() (metadata.Source, error) {
		calls++
		return nil, want
	})
	for i := 0; i < 3; i++ {
		if _, err := factory(); !errors.Is(err, want) {
			t.Fatalf("call %d err = %v, want %v", i, err, want)
		}
	}
	if calls != 1 {
		t.Fatalf("construct calls = %d, want 1", calls)
	}
}

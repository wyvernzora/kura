package workflow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wyvernzora/kura/services/library/internal/provider"
	"github.com/wyvernzora/kura/services/library/internal/textnorm"
	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

func TestProviderFactoryCachesSuccess(t *testing.T) {
	calls := 0
	factory := workflow.NewProviderFactory(func() (provider.Source, error) {
		calls++
		return nil, nil
	})
	for i := range 3 {
		if _, err := factory(); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if calls != 1 {
		t.Fatalf("construct calls = %d, want 1", calls)
	}
}

func TestProviderFactoryRetriesAfterFailure(t *testing.T) {
	calls := 0
	want := errors.New("missing key")
	factory := workflow.NewProviderFactory(func() (provider.Source, error) {
		calls++
		return nil, want
	})
	// Each call retries construct since prior attempts failed.
	for i := range 3 {
		if _, err := factory(); !errors.Is(err, want) {
			t.Fatalf("call %d err = %v, want %v", i, err, want)
		}
	}
	if calls != 3 {
		t.Fatalf("construct calls = %d, want 3 (failures must not be cached)", calls)
	}
}

type stubSource struct{}

func (stubSource) Key() string { return "stub" }
func (stubSource) Search(context.Context, textnorm.NFCString, provider.SearchOptions) ([]provider.SearchResult, error) {
	return nil, nil
}
func (stubSource) GetSeries(context.Context, string, string) (provider.Series, error) {
	return provider.Series{}, nil
}

func TestProviderFactoryCachesAfterRecoveredError(t *testing.T) {
	calls := 0
	wantErr := errors.New("transient")
	factory := workflow.NewProviderFactory(func() (provider.Source, error) {
		calls++
		if calls == 1 {
			return nil, wantErr
		}
		return stubSource{}, nil
	})

	if _, err := factory(); !errors.Is(err, wantErr) {
		t.Fatalf("first call err = %v, want %v", err, wantErr)
	}
	src1, err := factory()
	if err != nil {
		t.Fatalf("second call err = %v, want nil", err)
	}
	if src1 == nil {
		t.Fatal("second call src = nil, want non-nil")
	}
	src2, err := factory()
	if err != nil {
		t.Fatalf("third call err = %v, want nil", err)
	}
	if src2 != src1 {
		t.Fatal("third call returned different source; want cached")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (success caches; further calls reuse)", calls)
	}
}

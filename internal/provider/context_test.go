package provider

import (
	"context"
	"errors"
	"testing"
)

func TestSourceFromUnsetReturnsError(t *testing.T) {
	_, err := SourceFrom(context.Background())

	if !errors.Is(err, ErrMissingSource) {
		t.Fatalf("error = %v, want ErrMissingSource", err)
	}
}

func TestWithSourceMemoizesValue(t *testing.T) {
	want := &fakeSource{}
	calls := 0
	ctx := WithSource(context.Background(), func() (Source, error) {
		calls++
		return want, nil
	})

	first, err := SourceFrom(ctx)
	if err != nil {
		t.Fatalf("first SourceFrom: %v", err)
	}
	second, err := SourceFrom(ctx)
	if err != nil {
		t.Fatalf("second SourceFrom: %v", err)
	}

	if first != second {
		t.Fatal("SourceFrom returned different source instances")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestWithSourceMemoizesError(t *testing.T) {
	wantErr := errors.New("build failed")
	calls := 0
	ctx := WithSource(context.Background(), func() (Source, error) {
		calls++
		return nil, wantErr
	})

	_, firstErr := SourceFrom(ctx)
	_, secondErr := SourceFrom(ctx)

	if !errors.Is(firstErr, wantErr) || !errors.Is(secondErr, wantErr) {
		t.Fatalf("errors = %v, %v; want %v", firstErr, secondErr, wantErr)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

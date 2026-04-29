package resolve

import (
	"context"
	"errors"
	"testing"
)

func TestResolverFromUnsetReturnsError(t *testing.T) {
	_, err := ResolverFrom(context.Background())

	if !errors.Is(err, ErrMissingResolver) {
		t.Fatalf("error = %v, want ErrMissingResolver", err)
	}
}

func TestWithResolverMemoizesValue(t *testing.T) {
	want := New(NewMetadataIDStrategy(fakeMetadataSource{}))
	calls := 0
	ctx := WithResolver(context.Background(), func() (*Resolver, error) {
		calls++
		return want, nil
	})

	first, err := ResolverFrom(ctx)
	if err != nil {
		t.Fatalf("first ResolverFrom: %v", err)
	}
	second, err := ResolverFrom(ctx)
	if err != nil {
		t.Fatalf("second ResolverFrom: %v", err)
	}

	if first != second {
		t.Fatal("ResolverFrom returned different resolver instances")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestWithResolverMemoizesError(t *testing.T) {
	wantErr := errors.New("build failed")
	calls := 0
	ctx := WithResolver(context.Background(), func() (*Resolver, error) {
		calls++
		return nil, wantErr
	})

	_, firstErr := ResolverFrom(ctx)
	_, secondErr := ResolverFrom(ctx)

	if !errors.Is(firstErr, wantErr) || !errors.Is(secondErr, wantErr) {
		t.Fatalf("errors = %v, %v; want %v", firstErr, secondErr, wantErr)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

package metadata

import (
	"context"
	"errors"
	"sync"
)

type sourceContextKey struct{}

// ErrMissingSource is returned by SourceFrom when no Source builder has
// been attached to the context.
var ErrMissingSource = errors.New("no metadata source on context")

// WithSource attaches a lazily-built Source to ctx. The build function
// is invoked at most once across any number of SourceFrom calls on the
// returned context (or its descendants), and both success and failure
// are memoized.
func WithSource(ctx context.Context, build func() (Source, error)) context.Context {
	var once sync.Once
	var src Source
	var err error
	memoized := func() (Source, error) {
		once.Do(func() {
			if build == nil {
				err = ErrMissingSource
				return
			}
			src, err = build()
		})
		return src, err
	}
	return context.WithValue(ctx, sourceContextKey{}, memoized)
}

// SourceFrom returns the memoized Source for ctx, building it on first
// call. Returns ErrMissingSource if no builder was attached.
func SourceFrom(ctx context.Context) (Source, error) {
	if ctx == nil {
		return nil, ErrMissingSource
	}
	build, ok := ctx.Value(sourceContextKey{}).(func() (Source, error))
	if !ok {
		return nil, ErrMissingSource
	}
	return build()
}

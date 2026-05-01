package resolve

import (
	"context"
	"errors"
	"sync"
)

type resolverContextKey struct{}

// ErrMissingResolver is returned by ResolverFrom when no Resolver
// builder has been attached to the context.
var ErrMissingResolver = errors.New("no resolver on context")

// WithResolver attaches a lazily-built *Resolver to ctx. The build
// function is invoked at most once across any number of ResolverFrom
// calls on the returned context (or its descendants), and both success
// and failure are memoized.
func WithResolver(ctx context.Context, build func() (*Resolver, error)) context.Context {
	var once sync.Once
	var resolver *Resolver
	var err error
	memoized := func() (*Resolver, error) {
		once.Do(func() {
			if build == nil {
				err = ErrMissingResolver
				return
			}
			resolver, err = build()
		})
		return resolver, err
	}
	return context.WithValue(ctx, resolverContextKey{}, memoized)
}

// ResolverFrom returns the memoized Resolver for ctx, building it on
// first call. Returns ErrMissingResolver if no builder was attached.
func ResolverFrom(ctx context.Context) (*Resolver, error) {
	if ctx == nil {
		return nil, ErrMissingResolver
	}
	build, ok := ctx.Value(resolverContextKey{}).(func() (*Resolver, error))
	if !ok {
		return nil, ErrMissingResolver
	}
	return build()
}

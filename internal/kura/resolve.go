package kura

import (
	"context"

	"github.com/wyvernzora/kura/internal/resolve"
)

func (l *Library) Resolve(ctx context.Context, in ResolveInput) (Resolution, error) {
	resolver := resolve.New(
		resolve.NewMetadataIDStrategy(l.metadataSource),
		resolve.NewTextSearchStrategy(l.metadataSource),
	)
	return resolver.Resolve(ctx, resolve.ParseQuery(in.Terms))
}

package library

import (
	"context"

	"github.com/wyvernzora/kura/internal/resolve"
)

func (l *Library) Resolve(ctx context.Context, terms []string) (resolve.Resolution, error) {
	resolver := resolve.New(
		resolve.NewMetadataIDStrategy(l.source),
		resolve.NewTextSearchStrategy(l.source),
	)
	return resolver.Resolve(ctx, resolve.ParseQuery(terms))
}

package library

import (
	"context"
	"fmt"
	"strings"

	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/resolve"
)

func (l *Library) Resolve(ctx context.Context, terms []string) (resolve.Resolution, error) {
	progress.Start(ctx, "resolve", fmt.Sprintf("Resolving %s", strings.Join(terms, " ")), 0)
	resolver := resolve.New(
		resolve.NewMetadataIDStrategy(l.source),
		resolve.NewTextSearchStrategy(l.source),
	)
	resolution, err := resolver.Resolve(ctx, resolve.ParseQuery(terms))
	if err != nil {
		progress.Failure(ctx, "resolve", "Failed to resolve series", 0, 0)
		return resolve.Resolution{}, err
	}
	progress.Success(ctx, "resolve", "Resolved series", len(resolution.Results))
	return resolution, nil
}

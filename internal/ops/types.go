package ops

import (
	"context"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/store"
)

type MediaInspector interface {
	Inspect(context.Context, string) (domain.MediaInfo, error)
}

type MediaInspectorFunc func(context.Context, string) (domain.MediaInfo, error)

func (f MediaInspectorFunc) Inspect(ctx context.Context, path string) (domain.MediaInfo, error) {
	return f(ctx, path)
}

type MetadataSeriesResolver func(context.Context, store.Series) (metadata.Series, error)

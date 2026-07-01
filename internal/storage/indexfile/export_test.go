package indexfile

import (
	"context"

	"github.com/wyvernzora/kura/internal/domain/refs"
)

func (i *Index) SetEntryBuilderForTest(build func(context.Context, string, refs.Series) (Entry, error)) {
	i.builder = build
}

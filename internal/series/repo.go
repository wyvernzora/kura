package series

import (
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/series/state"
)

type repo struct {
	root string
}

func Initialize(root string, ref refs.Series, metadataRef refs.Metadata, metadataSeries metadata.Series) error {
	return state.Initialize(root, ref, metadataRef, metadataSeries)
}

func ReadMetadataRef(root string, ref refs.Series) (refs.Metadata, error) {
	return state.ReadMetadataRef(root, ref)
}

func (r repo) load(ref refs.Series) (seriesState, error) {
	return state.NewRepository(r.root).Load(ref)
}

func (r repo) save(ref refs.Series, model seriesState) error {
	return state.NewRepository(r.root).Save(ref, model)
}

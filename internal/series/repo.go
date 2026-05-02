package series

import (
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

type repo struct {
	root string
}

func Initialize(root string, ref refs.Series, metadataRef refs.Metadata, metadataSeries metadata.Series) error {
	return seriesfile.Initialize(root, ref, metadataRef, metadataSeries)
}

func (r repo) load(ref refs.Series) (seriesState, error) {
	model, err := seriesfile.Load(r.root, ref)
	if err != nil {
		return seriesState{}, err
	}
	return *model, nil
}

func (r repo) save(ref refs.Series, model seriesState) error {
	model.Ref = ref
	return seriesfile.Save(r.root, &model)
}

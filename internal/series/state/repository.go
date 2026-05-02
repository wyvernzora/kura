package state

import (
	"os"
	"path/filepath"

	"github.com/google/renameio/v2"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/series/wire"
)

type Repository struct {
	root string
}

func NewRepository(root string) Repository {
	return Repository{root: root}
}

func Initialize(root string, ref refs.Series, metadataRef refs.Metadata, metadataSeries metadata.Series) error {
	model, err := NewFromMetadata(metadataRef, metadataSeries)
	if err != nil {
		return err
	}
	return NewRepository(root).Save(ref, model)
}

func ReadMetadataRef(root string, ref refs.Series) (refs.Metadata, error) {
	series, err := NewRepository(root).Load(ref)
	if err != nil {
		return "", err
	}
	return series.Metadata, nil
}

func (r Repository) Load(ref refs.Series) (State, error) {
	path := wire.SeriesMetadataPath(filepath.Join(r.root, filepath.FromSlash(ref.String())))
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	decoded, err := wire.Decode(data)
	if err != nil {
		return State{}, err
	}
	return FromWire(decoded)
}

func (r Repository) Save(ref refs.Series, model State) error {
	encoded, err := ToWire(model)
	if err != nil {
		return err
	}
	data, err := wire.Encode(encoded)
	if err != nil {
		return err
	}
	seriesDir := filepath.Join(r.root, filepath.FromSlash(ref.String()))
	metaDir := filepath.Join(seriesDir, wire.KuraDir)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}
	return renameio.WriteFile(wire.SeriesMetadataPath(seriesDir), data, 0o644)
}

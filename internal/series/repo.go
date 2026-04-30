package series

import (
	"os"
	"path/filepath"

	"github.com/google/renameio/v2"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series/wire"
)

type repo struct {
	root string
}

func Save(root string, ref refs.Series, series Series) error {
	return repo{root: root}.save(ref, series)
}

func ReadMetadataRef(root string, ref refs.Series) (refs.Metadata, error) {
	series, err := repo{root: root}.load(ref)
	if err != nil {
		return "", err
	}
	return series.Metadata, nil
}

func (r repo) load(ref refs.Series) (Series, error) {
	path := wire.SeriesMetadataPath(filepath.Join(r.root, filepath.FromSlash(ref.String())))
	data, err := os.ReadFile(path)
	if err != nil {
		return Series{}, err
	}
	decoded, err := wire.Decode(data)
	if err != nil {
		return Series{}, err
	}
	return fromWire(decoded)
}

func (r repo) save(ref refs.Series, series Series) error {
	encoded, err := toWire(series)
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

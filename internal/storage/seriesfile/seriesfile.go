// Package seriesfile owns reading and writing series.json. Wire types are
// unexported; callers use *series.Series. Active record paths are absolute in
// memory and relative on disk; the package translates on Load and Save.
package seriesfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/renameio/v2"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/metadata"
)

// Load reads <libRoot>/<ref>/.kura/series.json, decodes it, absolutizes
// active record paths, and sets Ref on the returned *Series.
func Load(libRoot string, ref refs.Series) (*series.Series, error) {
	if ref.IsZero() {
		return nil, errors.New("seriesfile: ref is required")
	}
	seriesDir := seriesDirPath(libRoot, ref)
	data, err := os.ReadFile(metadataPath(seriesDir))
	if err != nil {
		return nil, err
	}
	wire, err := decode(data)
	if err != nil {
		return nil, err
	}
	model, err := fromWire(wire)
	if err != nil {
		return nil, err
	}
	model.Ref = ref
	absolutizeActive(model, seriesDir)
	return model, nil
}

// Save writes m to <libRoot>/<m.Ref>/.kura/series.json. Active record paths
// are relativized on disk; the in-memory *Series is not mutated. m.Ref must
// be set.
func Save(libRoot string, m *series.Series) error {
	if m == nil {
		return errors.New("seriesfile: Save called with nil Series")
	}
	if m.Ref.IsZero() {
		return errors.New("seriesfile: Save called with zero Ref")
	}
	seriesDir := seriesDirPath(libRoot, m.Ref)
	wire := toWire(m)
	if err := relativizeActiveWire(&wire, seriesDir); err != nil {
		return err
	}
	data, err := encode(wire)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(seriesDir, kuraDir), 0o755); err != nil {
		return err
	}
	return renameio.WriteFile(metadataPath(seriesDir), data, 0o644)
}

// Exists reports whether series.json is present at the canonical path. It
// distinguishes "not found" (false, nil) from stat errors (false, err).
func Exists(libRoot string, ref refs.Series) (bool, error) {
	seriesDir := seriesDirPath(libRoot, ref)
	_, err := os.Stat(metadataPath(seriesDir))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// NewFromMetadata builds a fresh *Series from provider metadata. Ref is left
// unset; callers must assign before Save.
func NewFromMetadata(metadataRef refs.Metadata, m metadata.Series) (*series.Series, error) {
	out := &series.Series{
		Metadata:       metadataRef,
		PreferredTitle: m.PreferredTitle,
		CanonicalTitle: m.CanonicalTitle,
		LastScanned:    time.Now().UTC(),
		Episodes:       map[refs.Episode]series.Episode{},
	}
	var spine []series.SpineEntry
	for _, season := range m.Seasons {
		for _, episode := range season.Episodes {
			if episode.Ref.IsZero() {
				return nil, fmt.Errorf("seriesfile: metadata has invalid episode ref")
			}
			airDate, err := series.ParseAirDate(episode.Aired)
			if err != nil {
				return nil, fmt.Errorf("seriesfile: invalid air date %q: %w", episode.Aired, err)
			}
			spine = append(spine, series.SpineEntry{Ref: episode.Ref, AirDate: airDate})
		}
	}
	out.RefreshSpine(spine)
	return out, nil
}

// Initialize is a convenience for "build from metadata + save." Sets Ref and
// writes to disk.
func Initialize(libRoot string, ref refs.Series, metadataRef refs.Metadata, m metadata.Series) error {
	model, err := NewFromMetadata(metadataRef, m)
	if err != nil {
		return err
	}
	model.Ref = ref
	return Save(libRoot, model)
}

// MetadataPath returns the canonical absolute path to series.json for ref
// under libRoot. Exposed because a couple of callers still need to remove
// the file directly during force-overwrite. Phase 3 centralizes path
// construction in storage/paths/.
func MetadataPath(libRoot string, ref refs.Series) string {
	return metadataPath(seriesDirPath(libRoot, ref))
}

func seriesDirPath(libRoot string, ref refs.Series) string {
	return filepath.Join(libRoot, filepath.FromSlash(ref.String()))
}

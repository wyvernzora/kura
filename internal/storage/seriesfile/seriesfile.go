// Package seriesfile owns reading and writing series.json. Wire types are
// unexported; callers use *series.Series. Active record paths are absolute in
// memory and relative on disk; the package translates on Load and Save.
//
// Coordination: Load populates Series.Hash with the SHA-256 of the file
// bytes; SaveCAS uses it as the expected on-disk hash for the optimistic
// check. Save (no CAS) is preserved for the migration window in commits 4-7
// but should be replaced with SaveCAS at all call sites by the end of
// phase 2 (plan/locking.md).
package seriesfile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/renameio/v2/maybe"
	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

// Load reads <libRoot>/<ref>/.kura/series.json, decodes it, absolutizes
// active record paths, sets Ref on the returned *Series, and populates
// Hash with the SHA-256 of the file bytes for use by SaveCAS.
func Load(libRoot string, ref refs.Series) (*series.Series, error) {
	if ref.IsZero() {
		return nil, errors.New("seriesfile: ref is required")
	}
	seriesDir := paths.SeriesDir(libRoot, ref)
	data, err := os.ReadFile(paths.SeriesMetadata(libRoot, ref))
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
	model.Hash = hashHex(data)
	absolutizeActive(model, seriesDir)
	return model, nil
}

// SaveCAS atomically writes m iff the on-disk file still hashes to m.Hash.
// Stamps mutator into LastMutated. Sets/clears InProgress as present in m.
//
// m.Hash == "" means "expect file does not exist; create via O_EXCL". Use
// this for the initial create path (add/import). The new file's hash is
// returned via m.Hash on success.
//
// Returns:
//   - *coord.ConflictError if disk hash != m.Hash (or file exists when m.Hash
//     is empty).
//   - os.ErrNotExist if m.Hash is non-empty but the file is gone.
//
// The ConflictError carries the winning side's last_mutated when readable.
func SaveCAS(libRoot string, m *series.Series, mutator coord.Mutator) error {
	if m == nil {
		return errors.New("seriesfile: SaveCAS called with nil Series")
	}
	if m.Ref.IsZero() {
		return errors.New("seriesfile: SaveCAS called with zero Ref")
	}
	scope := coord.SeriesScope(m.Ref)
	path := paths.SeriesMetadata(libRoot, m.Ref)

	if m.Hash == "" {
		return saveCASCreate(libRoot, m, mutator, path, scope)
	}
	return saveCASUpdate(libRoot, m, mutator, path, scope)
}

func saveCASCreate(libRoot string, m *series.Series, mutator coord.Mutator, path, scope string) error {
	if err := os.MkdirAll(paths.SeriesKuraDir(libRoot, m.Ref), 0o755); err != nil {
		return err
	}
	m.LastMutated = &mutator
	data, err := encodeForSeries(libRoot, m)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return &coord.ConflictError{
				Scope:   scope,
				Phase:   "pre_write",
				Mutator: peekMutator(path),
			}
		}
		return err
	}
	if _, writeErr := file.Write(data); writeErr != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return writeErr
	}
	if err := file.Close(); err != nil {
		return err
	}
	finalHash, err := readAndHash(path)
	if err != nil {
		return err
	}
	if finalHash != hashHex(data) {
		return &coord.ConflictError{Scope: scope, Phase: "post_write"}
	}
	m.Hash = finalHash
	return nil
}

func saveCASUpdate(libRoot string, m *series.Series, mutator coord.Mutator, path, scope string) error {
	currentBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	currentHash := hashHex(currentBytes)
	if currentHash != m.Hash {
		return &coord.ConflictError{
			Scope:   scope,
			Phase:   "pre_write",
			Mutator: peekMutatorBytes(currentBytes),
		}
	}
	m.LastMutated = &mutator
	data, err := encodeForSeries(libRoot, m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.SeriesKuraDir(libRoot, m.Ref), 0o755); err != nil {
		return err
	}
	if err := maybe.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	finalHash, err := readAndHash(path)
	if err != nil {
		return err
	}
	if finalHash != hashHex(data) {
		return &coord.ConflictError{Scope: scope, Phase: "post_write"}
	}
	m.Hash = finalHash
	return nil
}

func encodeForSeries(libRoot string, m *series.Series) ([]byte, error) {
	seriesDir := paths.SeriesDir(libRoot, m.Ref)
	wire := toWire(m)
	if err := relativizeActiveWire(&wire, seriesDir); err != nil {
		return nil, err
	}
	return encode(wire)
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readAndHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return hashHex(data), nil
}

// peekMutator best-effort reads last_mutated from path. Returns the zero
// Mutator if the file is unreadable, malformed, or has no last_mutated.
// Used to surface "lost race to" diagnostics in ConflictError.
func peekMutator(path string) coord.Mutator {
	data, err := os.ReadFile(path)
	if err != nil {
		return coord.Mutator{}
	}
	return peekMutatorBytes(data)
}

func peekMutatorBytes(data []byte) coord.Mutator {
	wire, err := decode(data)
	if err != nil {
		return coord.Mutator{}
	}
	if wire.LastMutated == nil {
		return coord.Mutator{}
	}
	mutator, err := mutatorFromWire(*wire.LastMutated)
	if err != nil {
		return coord.Mutator{}
	}
	return mutator
}

// ReadMetadataRef returns the metadataRef field of <libRoot>/<ref>/.kura/
// series.json without exposing the rest of the loaded model. Used by index
// rebuild to populate the metadataRef → seriesRef map cheaply.
func ReadMetadataRef(libRoot string, ref refs.Series) (refs.Metadata, error) {
	model, err := Load(libRoot, ref)
	if err != nil {
		return "", err
	}
	return model.Metadata, nil
}

// Exists reports whether series.json is present at the canonical path. It
// distinguishes "not found" (false, nil) from stat errors (false, err).
func Exists(libRoot string, ref refs.Series) (bool, error) {
	_, err := os.Stat(paths.SeriesMetadata(libRoot, ref))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// NewFromMetadata builds a fresh *Series from provider metadata. Ref is left
// unset; callers must assign before SaveCAS.
func NewFromMetadata(metadataRef refs.Metadata, m provider.Series) (*series.Series, error) {
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

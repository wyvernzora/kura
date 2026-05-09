// Package trashfile owns reading and writing per-trashed-media metadata at
// <library>/<series>/.kura/trash/<ulid>/meta.json. Trash records preserve
// the active record at the moment it was replaced, so the operator can
// recover or audit later.
package trashfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/renameio/v2/maybe"
	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

const schemaVersion = 1

// Meta is one trashed media event: which episode it belonged to, when it was
// trashed, and the record describing the displaced media.
type Meta struct {
	ID        ulid.ULID
	Episode   refs.Episode
	TrashedAt time.Time
	Record    Record
}

// Record mirrors the persisted shape of a media record at trash time.
// String-typed fields (Source/Resolution/Codec) match the wire shape used by
// series.json today; phase 7 may unify with domain/media types.
type Record struct {
	Path       string
	Source     string
	Resolution string
	Codec      string
	Size       int64
	MTime      time.Time
	Companions []Companion
}

type Companion struct {
	Path     string
	Role     string
	Language string
	Label    string
	Size     int64
	MTime    time.Time
}

type metaWire struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"ulid"`
	// EpisodeRef is the canonical storage form ("S01E0003") for media
	// associated with a specific episode (replaced active records,
	// trash-add of canonical files). Stagged-trash items queued from
	// the unified Stage flow may not be tied to an episode; in that
	// case the field is omitted on disk and decodes to a zero
	// refs.Episode in memory.
	EpisodeRef string     `json:"episodeRef,omitempty"`
	TrashedAt  string     `json:"trashedAt"`
	Record     recordWire `json:"record"`
}

type recordWire struct {
	Path       string          `json:"path"`
	Source     string          `json:"source"`
	Resolution string          `json:"resolution,omitempty"`
	Codec      string          `json:"codec,omitempty"`
	Size       int64           `json:"size"`
	MTime      string          `json:"mtime"`
	Companions []companionWire `json:"companions"`
}

type companionWire struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}

func Write(root string, ref refs.Series, m Meta) error {
	data, err := json.MarshalIndent(toWire(m), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := paths.TrashEntry(root, ref, m.ID.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return maybe.WriteFile(paths.TrashMeta(root, ref, m.ID.String()), data, 0o644)
}

func Read(root string, ref refs.Series, id ulid.ULID) (Meta, error) {
	data, err := os.ReadFile(paths.TrashMeta(root, ref, id.String()))
	if err != nil {
		return Meta{}, err
	}
	var wire metaWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return Meta{}, fmt.Errorf("trashfile: decode %s: %w", id, err)
	}
	return fromWire(wire)
}

// Delete removes the trash ULID directory and reports the bytes
// reclaimed. Returns (0, os.ErrNotExist) if the entry does not exist;
// returns the partial sum on RemoveAll failure.
func Delete(root string, ref refs.Series, id ulid.ULID) (int64, error) {
	dir := paths.TrashEntry(root, ref, id.String())
	bytes, err := dirSize(dir)
	if err != nil {
		return 0, err
	}
	if err := removeAllWithRetry(dir); err != nil {
		return 0, err
	}
	return bytes, nil
}

// removeAllWithRetry tolerates transient ENOTEMPTY / cached-dirent
// inconsistency on FUSE / bind-mount filesystems (Docker virtiofs,
// gRPC-FUSE, NFS). os.RemoveAll unlinks every child before rmdir-ing
// the parent, but on those FS layers the parent rmdir can race
// against still-propagating dirent metadata for a child that was
// just unlinked. A short retry loop with linear backoff lets the FS
// settle. Each attempt re-reads the directory and explicitly tries
// to unlink every leftover entry so the next RemoveAll has a clean
// directory to work with. ErrNotExist on re-list means the dir
// resolved itself (or another caller cleaned up); treat as success.
func removeAllWithRetry(dir string) error {
	const attempts = 4
	var last error
	for i := 0; i < attempts; i++ {
		err := os.RemoveAll(dir)
		if err == nil {
			return nil
		}
		last = err
		entries, readErr := os.ReadDir(dir)
		if errors.Is(readErr, os.ErrNotExist) {
			return nil
		}
		if readErr != nil {
			return last
		}
		for _, e := range entries {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
		time.Sleep(time.Duration(i+1) * 25 * time.Millisecond)
	}
	return last
}

// dirSize sums the file sizes immediately inside dir. Trash entries are
// flat (meta.json + media + companions) — no recursion needed.
func dirSize(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return total, err
		}
		total += info.Size()
	}
	return total, nil
}

func List(root string, ref refs.Series) ([]Meta, error) {
	dir := paths.TrashDir(root, ref)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Meta{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]Meta, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id, err := ulid.Parse(entry.Name())
		if err != nil {
			continue
		}
		meta, err := Read(root, ref, id)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func toWire(in Meta) metaWire {
	episodeRef := ""
	if !in.Episode.IsZero() {
		episodeRef = in.Episode.String()
	}
	out := metaWire{
		SchemaVersion: schemaVersion,
		ID:            in.ID.String(),
		EpisodeRef:    episodeRef,
		TrashedAt:     in.TrashedAt.UTC().Format(time.RFC3339),
		Record: recordWire{
			Path:       in.Record.Path,
			Source:     in.Record.Source,
			Resolution: in.Record.Resolution,
			Codec:      in.Record.Codec,
			Size:       in.Record.Size,
			MTime:      in.Record.MTime.UTC().Format(time.RFC3339),
			Companions: make([]companionWire, 0, len(in.Record.Companions)),
		},
	}
	for _, companion := range in.Record.Companions {
		out.Record.Companions = append(out.Record.Companions, companionWire{
			Path:     companion.Path,
			Role:     companion.Role,
			Language: companion.Language,
			Label:    companion.Label,
			Size:     companion.Size,
			MTime:    companion.MTime.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func fromWire(in metaWire) (Meta, error) {
	if in.SchemaVersion != schemaVersion {
		return Meta{}, fmt.Errorf("trashfile: unsupported schemaVersion %d", in.SchemaVersion)
	}
	id, err := ulid.Parse(in.ID)
	if err != nil {
		return Meta{}, err
	}
	var episode refs.Episode
	if in.EpisodeRef != "" {
		episode, err = refs.ParseEpisode(in.EpisodeRef)
		if err != nil {
			return Meta{}, err
		}
	}
	trashedAt, err := time.Parse(time.RFC3339, in.TrashedAt)
	if err != nil {
		return Meta{}, err
	}
	mtime, err := time.Parse(time.RFC3339, in.Record.MTime)
	if err != nil {
		return Meta{}, err
	}
	out := Meta{
		ID:        id,
		Episode:   episode,
		TrashedAt: trashedAt,
		Record: Record{
			Path:       in.Record.Path,
			Source:     in.Record.Source,
			Resolution: in.Record.Resolution,
			Codec:      in.Record.Codec,
			Size:       in.Record.Size,
			MTime:      mtime,
			Companions: make([]Companion, 0, len(in.Record.Companions)),
		},
	}
	for _, companion := range in.Record.Companions {
		mt, err := time.Parse(time.RFC3339, companion.MTime)
		if err != nil {
			return Meta{}, err
		}
		out.Record.Companions = append(out.Record.Companions, Companion{
			Path:     companion.Path,
			Role:     companion.Role,
			Language: companion.Language,
			Label:    companion.Label,
			Size:     companion.Size,
			MTime:    mt,
		})
	}
	return out, nil
}

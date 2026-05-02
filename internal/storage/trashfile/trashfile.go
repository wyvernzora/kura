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

	"github.com/google/renameio/v2"
	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

const (
	schemaVersion = 1
	kuraDir       = ".kura"
	DirName       = "trash"
	MetaFileName  = "meta.json"
)

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
	SchemaVersion int        `json:"schemaVersion"`
	ID            string     `json:"ulid"`
	EpisodeRef    string     `json:"episodeRef"`
	TrashedAt     string     `json:"trashedAt"`
	Record        recordWire `json:"record"`
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

func eventDir(root string, ref refs.Series, id ulid.ULID) string {
	return filepath.Join(root, filepath.FromSlash(ref.String()), kuraDir, DirName, id.String())
}

func metaPath(root string, ref refs.Series, id ulid.ULID) string {
	return filepath.Join(eventDir(root, ref, id), MetaFileName)
}

// EventDir is the absolute directory that holds one trashed media event's
// files. Media is moved into this directory; meta.json sits beside it.
func EventDir(root string, ref refs.Series, id ulid.ULID) string {
	return eventDir(root, ref, id)
}

// MetaPath is the absolute path to the meta.json for one trashed event.
func MetaPath(root string, ref refs.Series, id ulid.ULID) string {
	return metaPath(root, ref, id)
}

// MediaPath is the absolute path to a trashed media file inside an event
// directory.
func MediaPath(root string, ref refs.Series, id ulid.ULID, basename string) string {
	return filepath.Join(eventDir(root, ref, id), basename)
}

func Write(root string, ref refs.Series, m Meta) error {
	data, err := json.MarshalIndent(toWire(m), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := eventDir(root, ref, m.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return renameio.WriteFile(metaPath(root, ref, m.ID), data, 0o644)
}

func Read(root string, ref refs.Series, id ulid.ULID) (Meta, error) {
	data, err := os.ReadFile(metaPath(root, ref, id))
	if err != nil {
		return Meta{}, err
	}
	var wire metaWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return Meta{}, fmt.Errorf("trashfile: decode %s: %w", id, err)
	}
	return fromWire(wire)
}

func List(root string, ref refs.Series) ([]Meta, error) {
	dir := filepath.Join(root, filepath.FromSlash(ref.String()), kuraDir, DirName)
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
	out := metaWire{
		SchemaVersion: schemaVersion,
		ID:            in.ID.String(),
		EpisodeRef:    in.Episode.String(),
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
	episode, err := refs.ParseEpisode(in.EpisodeRef)
	if err != nil {
		return Meta{}, err
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

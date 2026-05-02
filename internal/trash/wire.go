package trash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/renameio/v2"
	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

const SchemaVersion = 1

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

func Write(root string, ref refs.Series, meta Meta) error {
	data, err := json.MarshalIndent(toWire(meta), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := EventDir(root, ref, meta.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return renameio.WriteFile(MetaPath(root, ref, meta.ID), data, 0o644)
}

func List(root string, ref refs.Series) ([]Meta, error) {
	dir := filepath.Join(root, filepath.FromSlash(ref.String()), ".kura", DirName)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
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
		data, err := os.ReadFile(MetaPath(root, ref, id))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var wire metaWire
		if err := json.Unmarshal(data, &wire); err != nil {
			return nil, err
		}
		meta, err := fromWire(wire)
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
		SchemaVersion: SchemaVersion,
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
	if in.SchemaVersion != SchemaVersion {
		return Meta{}, fmt.Errorf("unsupported trash meta schemaVersion %d", in.SchemaVersion)
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
		mtime, err := time.Parse(time.RFC3339, companion.MTime)
		if err != nil {
			return Meta{}, err
		}
		out.Record.Companions = append(out.Record.Companions, Companion{
			Path:     companion.Path,
			Role:     companion.Role,
			Language: companion.Language,
			Label:    companion.Label,
			Size:     companion.Size,
			MTime:    mtime,
		})
	}
	return out, nil
}

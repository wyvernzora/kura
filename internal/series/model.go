package series

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/refs"
)

// Series is the semantic in-memory model for one tracked series.
type Series struct {
	Metadata    refs.Metadata
	LastScanned time.Time
	Episodes    map[refs.Episode]Episode
}

// Episode is one persisted provider spine entry plus local media intent/state.
type Episode struct {
	AirDate string
	Active  *MediaRecord
	Staged  *MediaRecord
}

type MediaRecord struct {
	Path       string            `json:"path"`
	Source     string            `json:"source"`
	Resolution string            `json:"resolution,omitempty"`
	Codec      string            `json:"codec,omitempty"`
	Size       int64             `json:"size"`
	MTime      time.Time         `json:"mtime"`
	Companions []CompanionRecord `json:"companions"`
}

type CompanionRecord struct {
	Path     string    `json:"path"`
	Role     string    `json:"role,omitempty"`
	Language string    `json:"language,omitempty"`
	Label    string    `json:"label,omitempty"`
	Size     int64     `json:"size"`
	MTime    time.Time `json:"mtime"`
}

func (s Series) Clone() Series {
	out := Series{
		Metadata:    s.Metadata,
		LastScanned: s.LastScanned,
		Episodes:    make(map[refs.Episode]Episode, len(s.Episodes)),
	}
	for ref, episode := range s.Episodes {
		out.Episodes[ref] = cloneEpisode(episode)
	}
	return out
}

func cloneEpisode(in Episode) Episode {
	out := in
	if in.Active != nil {
		active := cloneMediaRecord(*in.Active)
		out.Active = &active
	}
	if in.Staged != nil {
		staged := cloneMediaRecord(*in.Staged)
		out.Staged = &staged
	}
	return out
}

func cloneMediaRecord(in MediaRecord) MediaRecord {
	out := in
	out.Companions = append([]CompanionRecord(nil), in.Companions...)
	if out.Companions == nil {
		out.Companions = []CompanionRecord{}
	}
	return out
}

func (s Series) MarshalJSON() ([]byte, error) {
	encoded, err := toWire(s)
	if err != nil {
		return nil, err
	}
	return json.Marshal(encoded)
}

func NewFromMetadata(ref refs.Metadata, metadataSeries metadata.Series) (Series, error) {
	out := Series{
		Metadata:    ref,
		LastScanned: time.Now().UTC(),
		Episodes:    map[refs.Episode]Episode{},
	}
	var spine []SpineEpisode
	for _, season := range metadataSeries.Seasons {
		for _, episode := range season.Episodes {
			if episode.Ref.IsZero() {
				return Series{}, fmt.Errorf("series: metadata has invalid episode ref")
			}
			spine = append(spine, SpineEpisode{Ref: episode.Ref, AirDate: episode.Aired})
		}
	}
	editor{series: &out}.refreshSpine(spine)
	return out, nil
}

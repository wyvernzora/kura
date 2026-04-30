package series

import (
	"encoding/json"
	"time"

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
	Path       string
	Source     string
	Resolution string
	Codec      string
	Size       int64
	MTime      time.Time
	Companions []CompanionRecord
}

type CompanionRecord struct {
	Path     string
	Role     string
	Language string
	Label    string
	Size     int64
	MTime    time.Time
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

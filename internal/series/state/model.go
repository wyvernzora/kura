package state

import (
	"fmt"
	"time"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/textnorm"
)

type State struct {
	Metadata       refs.Metadata
	PreferredTitle textnorm.NFCString
	CanonicalTitle textnorm.NFCString
	LastScanned    time.Time
	Episodes       map[refs.Episode]Episode
}

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

func CloneMediaRecord(in MediaRecord) MediaRecord {
	out := in
	out.Companions = append([]CompanionRecord(nil), in.Companions...)
	if out.Companions == nil {
		out.Companions = []CompanionRecord{}
	}
	return out
}

func NewFromMetadata(ref refs.Metadata, metadataSeries metadata.Series) (State, error) {
	out := State{
		Metadata:       ref,
		PreferredTitle: metadataSeries.PreferredTitle,
		CanonicalTitle: metadataSeries.CanonicalTitle,
		LastScanned:    time.Now().UTC(),
		Episodes:       map[refs.Episode]Episode{},
	}
	var spine []SpineEpisode
	for _, season := range metadataSeries.Seasons {
		for _, episode := range season.Episodes {
			if episode.Ref.IsZero() {
				return State{}, fmt.Errorf("series: metadata has invalid episode ref")
			}
			spine = append(spine, SpineEpisode{Ref: episode.Ref, AirDate: episode.Aired})
		}
	}
	Editor{Series: &out}.RefreshSpine(spine)
	return out, nil
}

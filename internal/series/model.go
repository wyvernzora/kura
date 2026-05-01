package series

import (
	"fmt"
	"time"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/textnorm"
)

// Series is the consumer-facing view of one tracked series.
type Series struct {
	MetadataRef    refs.Metadata       `json:"metadataRef"`
	Ref            refs.Series         `json:"ref"`
	Root           string              `json:"root"`
	PreferredTitle textnorm.NFCString  `json:"preferredTitle"`
	CanonicalTitle *textnorm.NFCString `json:"canonicalTitle,omitempty"`
	Seasons        []Season            `json:"seasons"`
}

type Season struct {
	MetadataRef refs.Metadata `json:"metadataRef,omitempty"`
	Number      int           `json:"number"`
	Episodes    []Episode     `json:"episodes"`
}

type Episode struct {
	MetadataRef    refs.Metadata `json:"metadataRef,omitempty"`
	Episode        refs.Episode  `json:"episode"`
	AbsoluteNumber *int          `json:"absoluteNumber,omitempty"`
	Aired          string        `json:"aired,omitempty"`
	Status         EpisodeStatus `json:"status"`
	Active         *EpisodeMedia `json:"active,omitempty"`
	Staged         *EpisodeMedia `json:"staged,omitempty"`
}

type EpisodeMedia struct {
	Source     string          `json:"source"`
	Resolution string          `json:"resolution,omitempty"`
	File       string          `json:"file"`
	Companions []CompanionFile `json:"companions"`
}

type CompanionFile struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}

type seriesState struct {
	Metadata    refs.Metadata
	LastScanned time.Time
	Episodes    map[refs.Episode]episodeState
}

type episodeState struct {
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

func cloneMediaRecord(in MediaRecord) MediaRecord {
	out := in
	out.Companions = append([]CompanionRecord(nil), in.Companions...)
	if out.Companions == nil {
		out.Companions = []CompanionRecord{}
	}
	return out
}

func newSeriesStateFromMetadata(ref refs.Metadata, metadataSeries metadata.Series) (seriesState, error) {
	out := seriesState{
		Metadata:    ref,
		LastScanned: time.Now().UTC(),
		Episodes:    map[refs.Episode]episodeState{},
	}
	var spine []SpineEpisode
	for _, season := range metadataSeries.Seasons {
		for _, episode := range season.Episodes {
			if episode.Ref.IsZero() {
				return seriesState{}, fmt.Errorf("series: metadata has invalid episode ref")
			}
			spine = append(spine, SpineEpisode{Ref: episode.Ref, AirDate: episode.Aired})
		}
	}
	editor{series: &out}.refreshSpine(spine)
	return out, nil
}

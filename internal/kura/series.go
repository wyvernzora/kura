package kura

import (
	"encoding/json"
	"time"

	"github.com/wyvernzora/kura/internal/domain"
	seriespkg "github.com/wyvernzora/kura/internal/series"
)

type Series struct {
	library *Library
	ref     SeriesRef
	model   seriespkg.Series
}

func newSeriesModel(library *Library, ref SeriesRef, model seriespkg.Series) *Series {
	return &Series{library: library, ref: ref, model: model}
}

func (s *Series) Ref() SeriesRef {
	return s.ref
}

func (s *Series) MetadataRef() MetadataRef {
	return MetadataRef(s.model.Metadata)
}

func (s *Series) PreferredTitle() string {
	return ""
}

func (s *Series) CanonicalTitle() string {
	return ""
}

func (s *Series) Episodes() []Episode {
	return modernEpisodes(s.model)
}

func (s *Series) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.model)
}

func modernEpisodes(model seriespkg.Series) []Episode {
	episodes := make([]Episode, 0, len(model.Episodes))
	for ref, episode := range model.Episodes {
		if episode.Active == nil {
			continue
		}
		episodes = append(episodes, Episode{
			Number: ref.Episode(),
			Media: MediaFile{
				Path:   episode.Active.Path,
				Source: episode.Active.Source,
				Size:   episode.Active.Size,
				MTime:  episode.Active.MTime.UTC().Format(time.RFC3339),
				MediaInfo: &domain.MediaInfo{
					Resolution: episode.Active.Resolution,
					VideoCodec: episode.Active.Codec,
				},
			},
			Companions: modernCompanions(episode.Active.Companions),
		})
	}
	if episodes == nil {
		return []Episode{}
	}
	return episodes
}

func modernCompanions(in []seriespkg.CompanionRecord) []CompanionFile {
	out := make([]CompanionFile, 0, len(in))
	for _, companion := range in {
		out = append(out, CompanionFile{
			Path:     companion.Path,
			Role:     companion.Role,
			Language: companion.Language,
			Label:    companion.Label,
			Size:     companion.Size,
			MTime:    companion.MTime.UTC().Format(time.RFC3339),
		})
	}
	if out == nil {
		return []CompanionFile{}
	}
	return out
}

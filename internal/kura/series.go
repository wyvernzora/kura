package kura

import (
	"encoding/json"

	seriespkg "github.com/wyvernzora/kura/internal/series"
	"github.com/wyvernzora/kura/internal/store"
)

type Series struct {
	library *Library
	ref     SeriesRef
	record  SeriesRecord
	model   seriespkg.Series
	modern  bool
}

func newSeries(library *Library, ref SeriesRef, record SeriesRecord) *Series {
	return &Series{library: library, ref: ref, record: record}
}

func newSeriesModel(library *Library, ref SeriesRef, model seriespkg.Series) *Series {
	return &Series{library: library, ref: ref, model: model, modern: true}
}

func (s *Series) Ref() SeriesRef {
	return s.ref
}

func (s *Series) MetadataRef() MetadataRef {
	if s.modern {
		return MetadataRef(s.model.Metadata)
	}
	return MetadataRef(s.record.MetadataRef)
}

func (s *Series) PreferredTitle() string {
	if s.modern {
		return ""
	}
	return s.record.PreferredTitle
}

func (s *Series) CanonicalTitle() string {
	if s.modern {
		return ""
	}
	return s.record.CanonicalTitle
}

func (s *Series) Episodes() []Episode {
	if s.modern {
		return modernEpisodes(s.model)
	}
	var episodes []Episode
	for _, season := range s.record.Seasons {
		for _, episode := range season.Episodes {
			episodes = append(episodes, copyEpisode(episode))
		}
	}
	if episodes == nil {
		return []Episode{}
	}
	return episodes
}

func (s *Series) MarshalJSON() ([]byte, error) {
	if s.modern {
		return json.Marshal(s.model)
	}
	record := copySeriesRecord(s.record)
	return json.Marshal(record)
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
				MTime:  episode.Active.MTime.UTC().Format("2006-01-02T15:04:05Z07:00"),
				MediaInfo: &store.MediaInfo{
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
			MTime:    companion.MTime.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	if out == nil {
		return []CompanionFile{}
	}
	return out
}

func copySeriesRecord(in SeriesRecord) SeriesRecord {
	out := in
	out.Seasons = append([]store.Season(nil), in.Seasons...)
	for seasonIndex := range out.Seasons {
		out.Seasons[seasonIndex].Episodes = append([]store.Episode(nil), in.Seasons[seasonIndex].Episodes...)
		for episodeIndex := range out.Seasons[seasonIndex].Episodes {
			out.Seasons[seasonIndex].Episodes[episodeIndex] = copyEpisode(out.Seasons[seasonIndex].Episodes[episodeIndex])
		}
	}
	return out
}

func copyEpisode(in Episode) Episode {
	out := in
	out.Companions = append([]CompanionFile(nil), in.Companions...)
	if out.Companions == nil {
		out.Companions = []CompanionFile{}
	}
	return out
}

package kura

import (
	"encoding/json"

	"github.com/wyvernzora/kura/internal/store"
)

type Series struct {
	library *Library
	ref     SeriesRef
	record  SeriesRecord
}

func newSeries(library *Library, ref SeriesRef, record SeriesRecord) *Series {
	return &Series{library: library, ref: ref, record: record}
}

func (s *Series) Ref() SeriesRef {
	return s.ref
}

func (s *Series) MetadataRef() MetadataRef {
	return MetadataRef(s.record.MetadataRef)
}

func (s *Series) PreferredTitle() string {
	return s.record.PreferredTitle
}

func (s *Series) CanonicalTitle() string {
	return s.record.CanonicalTitle
}

func (s *Series) Episodes() []Episode {
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
	record := copySeriesRecord(s.record)
	return json.Marshal(record)
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

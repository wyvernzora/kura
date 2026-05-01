package series

import (
	"fmt"

	"github.com/wyvernzora/kura/internal/refs"
)

type SpineEpisode struct {
	Ref     refs.Episode
	AirDate string
}

type editor struct {
	series *seriesState
}

func (e editor) refreshSpine(spine []SpineEpisode) {
	if e.series.Episodes == nil {
		e.series.Episodes = map[refs.Episode]episodeState{}
	}
	for _, incoming := range spine {
		episode := e.series.Episodes[incoming.Ref]
		episode.AirDate = incoming.AirDate
		e.series.Episodes[incoming.Ref] = episode
	}
}

func (e editor) setStaged(ref refs.Episode, record MediaRecord) error {
	episode, ok := e.series.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	record.Companions = append([]CompanionRecord(nil), record.Companions...)
	if record.Companions == nil {
		record.Companions = []CompanionRecord{}
	}
	episode.Staged = &record
	e.series.Episodes[ref] = episode
	return nil
}

func (e editor) promoteStaged(ref refs.Episode) (*MediaRecord, error) {
	episode, ok := e.series.Episodes[ref]
	if !ok {
		return nil, fmt.Errorf("series: metadata has no %s", ref)
	}
	if episode.Staged == nil {
		return nil, fmt.Errorf("series: %s has no staged media", ref)
	}
	var replaced *MediaRecord
	if episode.Active != nil {
		record := cloneMediaRecord(*episode.Active)
		replaced = &record
	}
	active := cloneMediaRecord(*episode.Staged)
	episode.Active = &active
	episode.Staged = nil
	e.series.Episodes[ref] = episode
	return replaced, nil
}

func (e editor) setActive(ref refs.Episode, record MediaRecord) error {
	episode, ok := e.series.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	record.Companions = append([]CompanionRecord(nil), record.Companions...)
	if record.Companions == nil {
		record.Companions = []CompanionRecord{}
	}
	episode.Active = &record
	e.series.Episodes[ref] = episode
	return nil
}

func (e editor) clearActive(ref refs.Episode) error {
	episode, ok := e.series.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	episode.Active = nil
	e.series.Episodes[ref] = episode
	return nil
}

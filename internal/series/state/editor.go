package state

import (
	"fmt"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

type SpineEpisode struct {
	Ref     refs.Episode
	AirDate string
}

type Editor struct {
	Series *State
}

func (e Editor) RefreshSpine(spine []SpineEpisode) {
	if e.Series.Episodes == nil {
		e.Series.Episodes = map[refs.Episode]Episode{}
	}
	for _, incoming := range spine {
		episode := e.Series.Episodes[incoming.Ref]
		episode.AirDate = incoming.AirDate
		e.Series.Episodes[incoming.Ref] = episode
	}
}

func (e Editor) SetStaged(ref refs.Episode, record media.Record) error {
	episode, ok := e.Series.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	record.Companions = append([]media.Companion(nil), record.Companions...)
	if record.Companions == nil {
		record.Companions = []media.Companion{}
	}
	episode.Staged = &record
	e.Series.Episodes[ref] = episode
	return nil
}

func (e Editor) ClearStaged(ref refs.Episode) error {
	episode, ok := e.Series.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	episode.Staged = nil
	e.Series.Episodes[ref] = episode
	return nil
}

func (e Editor) PromoteStaged(ref refs.Episode) (*media.Record, error) {
	episode, ok := e.Series.Episodes[ref]
	if !ok {
		return nil, fmt.Errorf("series: metadata has no %s", ref)
	}
	if episode.Staged == nil {
		return nil, fmt.Errorf("series: %s has no staged media", ref)
	}
	var replaced *media.Record
	if episode.Active != nil {
		record := media.CloneRecord(*episode.Active)
		replaced = &record
	}
	active := media.CloneRecord(*episode.Staged)
	episode.Active = &active
	episode.Staged = nil
	e.Series.Episodes[ref] = episode
	return replaced, nil
}

func (e Editor) SetActive(ref refs.Episode, record media.Record) error {
	episode, ok := e.Series.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	record.Companions = append([]media.Companion(nil), record.Companions...)
	if record.Companions == nil {
		record.Companions = []media.Companion{}
	}
	episode.Active = &record
	e.Series.Episodes[ref] = episode
	return nil
}

func (e Editor) ClearActive(ref refs.Episode) error {
	episode, ok := e.Series.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	episode.Active = nil
	e.Series.Episodes[ref] = episode
	return nil
}

// Package series defines the in-memory shape of a tracked series and the
// pure operations on it. No IO. Translation between this shape and the
// on-disk wire format lives in storage/seriesfile.
package series

import (
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/textnorm"
)

// ParseAirDate parses YYYY-MM-DD or empty into a civil.Date. Empty input
// returns the zero value (civil.Date.IsValid() reports false).
func ParseAirDate(value string) (civil.Date, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return civil.Date{}, nil
	}
	return civil.ParseDate(value)
}

// Series is the persisted shape of a tracked series in memory. It is
// round-trippable through seriesfile.Load/Save.
//
// Ref is the series filesystem ref the loader read this Series from. It is
// not part of the wire format; seriesfile.Load populates it after successful
// decode so call sites do not need to track the ref alongside *Series.
type Series struct {
	Ref            refs.Series
	Metadata       refs.Metadata
	PreferredTitle textnorm.NFCString
	CanonicalTitle textnorm.NFCString
	LastScanned    time.Time
	Episodes       map[refs.Episode]Episode
}

// Episode is the persisted shape for one episode slot.
type Episode struct {
	AirDate civil.Date
	Active  *media.Record
	Staged  *media.Record
}

// SpineEntry describes one episode slot in the metadata-derived spine. Used
// to seed or refresh a Series's Episodes.
type SpineEntry struct {
	Ref     refs.Episode
	AirDate civil.Date
}

// RefreshSpine adds new spine entries and updates known air dates without
// removing any existing episodes.
func (s *Series) RefreshSpine(spine []SpineEntry) {
	if s.Episodes == nil {
		s.Episodes = map[refs.Episode]Episode{}
	}
	for _, incoming := range spine {
		episode := s.Episodes[incoming.Ref]
		episode.AirDate = incoming.AirDate
		s.Episodes[incoming.Ref] = episode
	}
}

func (s *Series) SetStaged(ref refs.Episode, record media.Record) error {
	episode, ok := s.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	record.Companions = append([]media.Companion(nil), record.Companions...)
	if record.Companions == nil {
		record.Companions = []media.Companion{}
	}
	episode.Staged = &record
	s.Episodes[ref] = episode
	return nil
}

func (s *Series) ClearStaged(ref refs.Episode) error {
	episode, ok := s.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	episode.Staged = nil
	s.Episodes[ref] = episode
	return nil
}

// PromoteStaged moves the staged record into the active slot and clears
// staged. Returns the previous active record (if any) so callers can track it
// for trash.
func (s *Series) PromoteStaged(ref refs.Episode) (*media.Record, error) {
	episode, ok := s.Episodes[ref]
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
	s.Episodes[ref] = episode
	return replaced, nil
}

func (s *Series) SetActive(ref refs.Episode, record media.Record) error {
	episode, ok := s.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	record.Companions = append([]media.Companion(nil), record.Companions...)
	if record.Companions == nil {
		record.Companions = []media.Companion{}
	}
	episode.Active = &record
	s.Episodes[ref] = episode
	return nil
}

func (s *Series) ClearActive(ref refs.Episode) error {
	episode, ok := s.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	episode.Active = nil
	s.Episodes[ref] = episode
	return nil
}

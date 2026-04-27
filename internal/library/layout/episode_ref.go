package layout

import (
	"fmt"
	"strconv"
	"strings"
)

// SeasonNumber is an episode identity season number. Season 0 represents
// specials, even though specials are stored separately in Series.Specials.
type SeasonNumber int

func NewSeasonNumber(number int) (SeasonNumber, error) {
	if number < 0 {
		return 0, fmt.Errorf("library: invalid season number %d", number)
	}
	return SeasonNumber(number), nil
}

func ParseSeasonNumber(value string) (SeasonNumber, error) {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("library: invalid season number %q", value)
	}
	return NewSeasonNumber(number)
}

func RegularSeason(number int) (SeasonNumber, error) {
	if number < 1 {
		return 0, fmt.Errorf("library: invalid regular season number %d", number)
	}
	return SeasonNumber(number), nil
}

func SpecialsSeason() SeasonNumber {
	return SeasonNumber(0)
}

func (s SeasonNumber) Int() int {
	return int(s)
}

func (s SeasonNumber) IsSpecial() bool {
	return s == 0
}

func (s SeasonNumber) MarkerPart() string {
	return fmt.Sprintf("S%02d", s)
}

// EpisodeNumber is a positive episode number.
type EpisodeNumber int

func NewEpisodeNumber(number int) (EpisodeNumber, error) {
	if number < 1 {
		return 0, fmt.Errorf("library: invalid episode number %d", number)
	}
	return EpisodeNumber(number), nil
}

func ParseEpisodeNumber(value string) (EpisodeNumber, error) {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("library: invalid episode number %q", value)
	}
	return NewEpisodeNumber(number)
}

func (e EpisodeNumber) Int() int {
	return int(e)
}

func (e EpisodeNumber) MarkerPart() string {
	return fmt.Sprintf("E%02d", e)
}

// EpisodeRef is a season/episode pair such as S02E03 or S00E01.
type EpisodeRef struct {
	season  SeasonNumber
	episode EpisodeNumber
}

func NewEpisodeRef(season SeasonNumber, episode EpisodeNumber) EpisodeRef {
	return EpisodeRef{season: season, episode: episode}
}

func (r EpisodeRef) Season() SeasonNumber {
	return r.season
}

func (r EpisodeRef) Episode() EpisodeNumber {
	return r.episode
}

func (r EpisodeRef) Marker() string {
	return r.season.MarkerPart() + r.episode.MarkerPart()
}

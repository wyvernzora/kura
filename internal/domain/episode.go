package domain

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wyvernzora/kura/internal/refs"
)

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

func (r EpisodeRef) StorageRef() refs.Episode {
	ref, _ := refs.NewEpisode(r.season.Int(), r.episode.Int())
	return ref
}

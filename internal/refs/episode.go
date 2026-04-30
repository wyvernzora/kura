package refs

import (
	"fmt"
	"regexp"
	"strconv"
)

var episodePattern = regexp.MustCompile(`^S([0-9]{2,})E([0-9]{4,})$`)

// Episode identifies a local episode slot. Its string form is fixed-width for
// storage so lexical ordering matches natural episode order.
type Episode struct {
	season  int
	episode int
}

func NewEpisode(season, episode int) (Episode, error) {
	if season < 0 {
		return Episode{}, fmt.Errorf("library: invalid season number %d", season)
	}
	if episode < 1 {
		return Episode{}, fmt.Errorf("library: invalid episode number %d", episode)
	}
	return Episode{season: season, episode: episode}, nil
}

func ParseEpisode(value string) (Episode, error) {
	match := episodePattern.FindStringSubmatch(value)
	if match == nil {
		return Episode{}, invalid("episode ref", value, "expected S<NN>E<NNNN>")
	}
	season, err := strconv.Atoi(match[1])
	if err != nil {
		return Episode{}, err
	}
	episode, err := strconv.Atoi(match[2])
	if err != nil {
		return Episode{}, err
	}
	return NewEpisode(season, episode)
}

func (ref Episode) Season() int {
	return ref.season
}

func (ref Episode) Episode() int {
	return ref.episode
}

func (ref Episode) String() string {
	return fmt.Sprintf("S%02dE%04d", ref.season, ref.episode)
}

// Marker returns the filename-oriented marker with minimum two-digit episode
// padding, for example S01E01.
func (ref Episode) Marker() string {
	return fmt.Sprintf("S%02dE%02d", ref.season, ref.episode)
}

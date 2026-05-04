package refs

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
)

var episodePattern = regexp.MustCompile(`^S([0-9]{2,})E([0-9]{4,})$`)

// Episode identifies one episode in the local series spine. Its string form is
// fixed-width for storage so lexical ordering matches natural episode order.
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
		return Episode{}, fmt.Errorf("invalid episode ref %q; expected S<NN>E<NNNN>", value)
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

// markerPattern matches the relaxed display form (variable padding
// 1-5 digits per side) accepted from human / agent input. Storage
// always normalizes back to the strict 4-digit-episode form via
// NewEpisode.
var markerPattern = regexp.MustCompile(`^S([0-9]{1,5})E([0-9]{1,5})$`)

// ParseEpisodeMarker accepts either the canonical storage form
// (S<NN>E<NNNN>, e.g. "S01E0003") or the relaxed display marker
// (S<N>E<N>, e.g. "S1E3" / "S01E03"). Both normalize to the same
// Episode value via NewEpisode.
func ParseEpisodeMarker(value string) (Episode, error) {
	if ref, err := ParseEpisode(value); err == nil {
		return ref, nil
	}
	match := markerPattern.FindStringSubmatch(value)
	if match == nil {
		return Episode{}, fmt.Errorf("invalid episode %q; expected marker S01E03 or storage form S01E0003", value)
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

func (ref Episode) IsSpecial() bool {
	return ref.season == 0
}

func (ref Episode) IsZero() bool {
	return ref.season == 0 && ref.episode == 0
}

func (ref Episode) String() string {
	return fmt.Sprintf("S%02dE%04d", ref.season, ref.episode)
}

// Marker returns the filename-oriented marker with minimum two-digit episode
// padding, for example S01E01.
func (ref Episode) Marker() string {
	return fmt.Sprintf("S%02dE%02d", ref.season, ref.episode)
}

func (ref Episode) MarshalJSON() ([]byte, error) {
	return json.Marshal(ref.String())
}

func (ref *Episode) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := ParseEpisode(value)
	if err != nil {
		return err
	}
	*ref = parsed
	return nil
}

func (ref Episode) MarshalText() ([]byte, error) {
	return []byte(ref.String()), nil
}

func (ref *Episode) UnmarshalText(data []byte) error {
	parsed, err := ParseEpisode(string(data))
	if err != nil {
		return err
	}
	*ref = parsed
	return nil
}

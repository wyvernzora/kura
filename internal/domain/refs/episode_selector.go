package refs

import (
	"fmt"
	"regexp"
	"strconv"
)

// EpisodeSelector restricts a set of Episode refs to one season,
// optionally to a [From, To] inclusive episode-number range. Used by
// `kura_show` and the CLI's `--episodes` flag to scope output to a
// portion of a series's spine.
//
// Grammar:
//
//	S<N>             - all episodes in season N (HasRange = false).
//	S<N>E<E>         - single episode S<N>E<E> (HasRange = true, From = To = E).
//	S<N>E<A>-<B>     - range [A, B] inclusive (HasRange = true).
//
// Specials live in season 0; spell as `S0`. Numbers accept any width
// (1-5 digits) to match the existing relaxed episode-marker grammar.
type EpisodeSelector struct {
	Active   bool // true when the selector should filter; false = no-op
	Season   int
	HasRange bool
	From     int
	To       int
}

// IsZero reports whether the selector was produced by parsing an empty
// string (i.e. caller wants no episode-axis filter). Equivalent to
// !sel.Active; provided for symmetry with other refs types.
func (sel EpisodeSelector) IsZero() bool {
	return !sel.Active
}

// Matches reports whether ref falls within the selector. An inactive
// selector matches everything; callers can pre-check IsZero to skip
// the call entirely on the hot path.
func (sel EpisodeSelector) Matches(ref Episode) bool {
	if !sel.Active {
		return true
	}
	if ref.Season() != sel.Season {
		return false
	}
	if !sel.HasRange {
		return true
	}
	n := ref.Episode()
	return n >= sel.From && n <= sel.To
}

// String returns the canonical selector string. Useful for round-trip
// and for surfacing selector-shaped values in responses (e.g. truncated
// ranges in `kura_show`).
func (sel EpisodeSelector) String() string {
	if !sel.Active {
		return ""
	}
	if !sel.HasRange {
		return fmt.Sprintf("S%d", sel.Season)
	}
	if sel.From == sel.To {
		return fmt.Sprintf("S%dE%d", sel.Season, sel.From)
	}
	return fmt.Sprintf("S%dE%d-%d", sel.Season, sel.From, sel.To)
}

// InvalidEpisodeSelectorError signals a malformed selector string or a
// semantically invalid range (start > end).
type InvalidEpisodeSelectorError struct {
	Input  string
	Reason string
}

func (e *InvalidEpisodeSelectorError) Error() string {
	return fmt.Sprintf("invalid episode selector %q: %s", e.Input, e.Reason)
}

var (
	selectorSeasonPattern = regexp.MustCompile(`^S([0-9]{1,5})$`)
	selectorEpPattern     = regexp.MustCompile(`^S([0-9]{1,5})E([0-9]{1,5})$`)
	selectorRangePattern  = regexp.MustCompile(`^S([0-9]{1,5})E([0-9]{1,5})-([0-9]{1,5})$`)
)

// ParseEpisodeSelector parses one of the three selector forms. Empty
// input returns the zero EpisodeSelector + nil error (caller treats
// that as "no filter").
func ParseEpisodeSelector(input string) (EpisodeSelector, error) {
	if input == "" {
		return EpisodeSelector{}, nil
	}
	if m := selectorRangePattern.FindStringSubmatch(input); m != nil {
		season, _ := strconv.Atoi(m[1])
		from, _ := strconv.Atoi(m[2])
		to, _ := strconv.Atoi(m[3])
		if from < 1 || to < 1 {
			return EpisodeSelector{}, &InvalidEpisodeSelectorError{Input: input, Reason: "episode numbers must be >= 1"}
		}
		if from > to {
			return EpisodeSelector{}, &InvalidEpisodeSelectorError{Input: input, Reason: "range start > end"}
		}
		return EpisodeSelector{Active: true, Season: season, HasRange: true, From: from, To: to}, nil
	}
	if m := selectorEpPattern.FindStringSubmatch(input); m != nil {
		season, _ := strconv.Atoi(m[1])
		ep, _ := strconv.Atoi(m[2])
		if ep < 1 {
			return EpisodeSelector{}, &InvalidEpisodeSelectorError{Input: input, Reason: "episode number must be >= 1"}
		}
		return EpisodeSelector{Active: true, Season: season, HasRange: true, From: ep, To: ep}, nil
	}
	if m := selectorSeasonPattern.FindStringSubmatch(input); m != nil {
		season, _ := strconv.Atoi(m[1])
		return EpisodeSelector{Active: true, Season: season}, nil
	}
	return EpisodeSelector{}, &InvalidEpisodeSelectorError{
		Input:  input,
		Reason: "expected S<N>, S<N>E<E>, or S<N>E<A>-<B>",
	}
}

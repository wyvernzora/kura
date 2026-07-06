package refs

import (
	"fmt"
	"regexp"
	"strconv"
)

// EpisodeSelectorKind identifies which episode selector grammar branch was parsed.
type EpisodeSelectorKind int

const (
	// EpisodeSelectorDefault is the zero value: omitted input, equivalent to ALL.
	EpisodeSelectorDefault EpisodeSelectorKind = iota
	// EpisodeSelectorAll selects every episode row, including specials.
	EpisodeSelectorAll
	// EpisodeSelectorNormal selects a concrete season, episode, or range.
	EpisodeSelectorNormal
	// EpisodeSelectorNone selects no episode rows.
	EpisodeSelectorNone
	// EpisodeSelectorAiringSeason selects seasons via Kura's airing-window rules.
	EpisodeSelectorAiringSeason
)

// EpisodeSelector restricts a set of Episode refs to one season,
// optionally to a [From, To] inclusive episode-number range. Used by
// `kura_show` and the CLI's `--episodes` flag to scope output to a
// portion of a series's spine.
//
// Grammar:
//
//	ALL              - all episodes; equivalent to empty input.
//	NONE             - no episode rows.
//	AIRING_SEASON    - season(s) currently in the airing window.
//	S<N>             - all episodes in season N (HasRange = false).
//	S<N>E<E>         - single episode S<N>E<E> (HasRange = true, From = To = E).
//	S<N>E<A>-<B>     - range [A, B] inclusive (HasRange = true).
//
// Specials live in season 0; spell as `S0`. Numbers accept any width
// (1-5 digits) to match the existing relaxed episode-marker grammar.
type EpisodeSelector struct {
	Kind     EpisodeSelectorKind
	Season   int
	HasRange bool
	From     int
	To       int
}

// IsZero reports whether the selector was produced by parsing an empty
// string (i.e. caller wants the default ALL episode-axis behavior).
// Explicit selectors such as ALL and NONE are not zero.
func (sel EpisodeSelector) IsZero() bool {
	return sel.Kind == EpisodeSelectorDefault
}

func (sel EpisodeSelector) IsNone() bool {
	return sel.Kind == EpisodeSelectorNone
}

func (sel EpisodeSelector) IsAiringSeason() bool {
	return sel.Kind == EpisodeSelectorAiringSeason
}

func (sel EpisodeSelector) IsNormal() bool {
	return sel.Kind == EpisodeSelectorNormal
}

// Matches reports whether ref falls within the selector. An inactive
// selector matches everything; callers can pre-check IsZero to skip
// the call entirely on the hot path.
func (sel EpisodeSelector) Matches(ref Episode) bool {
	switch sel.Kind {
	case EpisodeSelectorDefault, EpisodeSelectorAll:
		return true
	case EpisodeSelectorNone:
		return false
	case EpisodeSelectorAiringSeason:
		panic("refs: AIRING_SEASON requires air-date context")
	default:
		if ref.Season() != sel.Season {
			return false
		}
		if !sel.HasRange {
			return true
		}
		n := ref.Episode()
		return n >= sel.From && n <= sel.To
	}
}

// String returns the canonical selector string. Useful for round-trip
// and for surfacing selector-shaped values in responses (e.g. truncated
// ranges in `kura_show`).
func (sel EpisodeSelector) String() string {
	switch sel.Kind {
	case EpisodeSelectorDefault:
		return ""
	case EpisodeSelectorAll:
		return "ALL"
	case EpisodeSelectorNone:
		return "NONE"
	case EpisodeSelectorAiringSeason:
		return "AIRING_SEASON"
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

// ParseEpisodeSelector parses a keyword, season, episode, or range
// selector. Empty input returns the zero EpisodeSelector + nil error
// (caller treats that as ALL).
func ParseEpisodeSelector(input string) (EpisodeSelector, error) {
	if input == "" {
		return EpisodeSelector{}, nil
	}
	switch input {
	case "ALL":
		return EpisodeSelector{Kind: EpisodeSelectorAll}, nil
	case "NONE":
		return EpisodeSelector{Kind: EpisodeSelectorNone}, nil
	case "AIRING_SEASON":
		return EpisodeSelector{Kind: EpisodeSelectorAiringSeason}, nil
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
		return EpisodeSelector{Kind: EpisodeSelectorNormal, Season: season, HasRange: true, From: from, To: to}, nil
	}
	if m := selectorEpPattern.FindStringSubmatch(input); m != nil {
		season, _ := strconv.Atoi(m[1])
		ep, _ := strconv.Atoi(m[2])
		if ep < 1 {
			return EpisodeSelector{}, &InvalidEpisodeSelectorError{Input: input, Reason: "episode number must be >= 1"}
		}
		return EpisodeSelector{Kind: EpisodeSelectorNormal, Season: season, HasRange: true, From: ep, To: ep}, nil
	}
	if m := selectorSeasonPattern.FindStringSubmatch(input); m != nil {
		season, _ := strconv.Atoi(m[1])
		return EpisodeSelector{Kind: EpisodeSelectorNormal, Season: season}, nil
	}
	return EpisodeSelector{}, &InvalidEpisodeSelectorError{
		Input:  input,
		Reason: "expected ALL, NONE, AIRING_SEASON, S<N>, S<N>E<E>, or S<N>E<A>-<B>",
	}
}

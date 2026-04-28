package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// SeasonNumber is an episode identity season number. Season 0 represents
// specials.
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

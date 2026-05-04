package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseDuration parses a "<integer><unit>" age string used by CLI flags
// such as --older-than. Supported units: s, m, h, d, w. Whitespace is
// trimmed; the empty string returns 0 with no error so callers can treat
// "not supplied" and "no filter" the same way.
//
// The standard library time.ParseDuration does not accept "d" or "w",
// which makes "30d" awkward to express as "720h" in operator-facing
// flags. This helper covers the common cases.
func ParseDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	if len(value) < 2 {
		return 0, fmt.Errorf("invalid duration %q: expected <integer><unit> with unit s/m/h/d/w", value)
	}
	unit := value[len(value)-1]
	number, err := strconv.Atoi(value[:len(value)-1])
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", value, err)
	}
	if number < 0 {
		return 0, fmt.Errorf("invalid duration %q: must be non-negative", value)
	}
	switch unit {
	case 's':
		return time.Duration(number) * time.Second, nil
	case 'm':
		return time.Duration(number) * time.Minute, nil
	case 'h':
		return time.Duration(number) * time.Hour, nil
	case 'd':
		return time.Duration(number) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(number) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid duration %q: unknown unit %q (want s/m/h/d/w)", value, string(unit))
	}
}

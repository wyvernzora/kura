package crawl

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"time"
)

// extDurationPrefixRe strips a leading <int>w and/or <int>d (weeks/days) prefix off an
// extended duration string. time.ParseDuration tops out at hours, so days/weeks are
// handled here and the remainder delegated to the standard parser.
var extDurationPrefixRe = regexp.MustCompile(`^(?:(\d+)w)?(?:(\d+)d)?`)

// ParseLookback parses an extended Go duration: an optional leading <int>w and/or
// <int>d (weeks/days) prefix is stripped and added on, the remainder delegated to
// time.ParseDuration (so 30d, 2w, 36h, 90m, 2w12h all parse). "" or a result of 0
// means "no lookback limit" (0). A malformed input (negatives, trailing garbage,
// anything time.ParseDuration rejects) is a hard error - the caller maps it to a 400
// client error, never a 502.
func ParseLookback(source, s string) (time.Duration, error) {
	if s == "" || s == "0" {
		return 0, nil
	}

	m := extDurationPrefixRe.FindStringSubmatch(s)
	// m[0] is the matched prefix (possibly empty); m[1]=weeks, m[2]=days.
	var ext time.Duration
	if m[1] != "" {
		w, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s: malformed lookback %q: %w", source, s, err)
		}
		if w > uint64(math.MaxInt64/int64(7*24*time.Hour)) {
			return 0, fmt.Errorf("%s: malformed lookback %q: overflows time.Duration", source, s)
		}
		ext = time.Duration(w) * 7 * 24 * time.Hour
	}
	if m[2] != "" {
		d, err := strconv.ParseUint(m[2], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s: malformed lookback %q: %w", source, s, err)
		}
		if d > uint64((math.MaxInt64-int64(ext))/int64(24*time.Hour)) {
			return 0, fmt.Errorf("%s: malformed lookback %q: overflows time.Duration", source, s)
		}
		ext += time.Duration(d) * 24 * time.Hour
	}

	rest := s[len(m[0]):]
	if rest != "" {
		d, err := time.ParseDuration(rest)
		if err != nil {
			return 0, fmt.Errorf("%s: malformed lookback %q: %w", source, s, err)
		}
		if d < 0 {
			return 0, fmt.Errorf("%s: malformed lookback %q: negative duration", source, s)
		}
		if d > time.Duration(math.MaxInt64-int64(ext)) {
			return 0, fmt.Errorf("%s: malformed lookback %q: overflows time.Duration", source, s)
		}
		ext += d
	}

	return ext, nil
}

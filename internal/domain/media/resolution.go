package media

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Resolution struct {
	width  int
	height int
}

var resolutionPattern = regexp.MustCompile(`^([0-9]+)x([0-9]+)$`)

func NewResolution(width, height int) (Resolution, error) {
	if width < 1 || height < 1 {
		return Resolution{}, fmt.Errorf("library: invalid resolution %dx%d", width, height)
	}
	return Resolution{width: width, height: height}, nil
}

func ParseResolution(value string) (Resolution, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Resolution{}, nil
	}
	matches := resolutionPattern.FindStringSubmatch(value)
	if len(matches) != 3 {
		return Resolution{}, fmt.Errorf("library: invalid resolution %q", value)
	}
	width, _ := strconv.Atoi(matches[1])
	height, _ := strconv.Atoi(matches[2])
	return NewResolution(width, height)
}

func (r Resolution) Width() int {
	return r.width
}

func (r Resolution) Height() int {
	return r.height
}

func (r Resolution) Known() bool {
	return r.width > 0 && r.height > 0
}

func (r Resolution) String() string {
	if !r.Known() {
		return ""
	}
	return fmt.Sprintf("%dx%d", r.width, r.height)
}

func (r Resolution) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

func (r *Resolution) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseResolution(s)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

// Display returns the human-friendly label for the resolution. Folds
// non-canonical dimensions into the nearest standard tier so the
// operator sees actionable labels rather than raw pixel counts. Three
// fold cases motivate the bucketing:
//
//   - 4:3 variants (1440x1080, 960x720): operator can't get 16:9, so
//     the height carries the tier.
//   - Cinemascope / letterboxed crops (1920x800, 2560x1080): the
//     vertical was cropped from a higher source, so the width carries
//     the tier.
//   - Near-standard ±5% encodes (1328x720, 1272x712, 1916x1076):
//     loose bucket boundaries absorb the variation.
//
// Bucket assignment is the higher of the height-derived and
// width-derived tier. Raw WxH stays available via String() / the JSON
// serialization for callers that need exact dimensions.
//
// Anything below 340 lines AND 600 wide falls through to raw WxH —
// SD-PAL / VHS-tier sources are rare enough to deserve the literal
// number rather than a misleading shorthand.
func (r Resolution) Display() string {
	if !r.Known() {
		return ""
	}
	tier := heightTier(r.height)
	if widthTier(r.width) > tier {
		tier = widthTier(r.width)
	}
	if tier == 0 {
		return r.String()
	}
	return tierLabel(tier)
}

// Resolution tiers, ordered low → high so max() picks the higher
// tier when height and width disagree (e.g. cinemascope crops where
// width signals 1080p but height signals 720p).
const (
	tierBelow360 = 0
	tier360p     = 1
	tier480p     = 2
	tier720p     = 3
	tier1080p    = 4
	tier1440p    = 5
	tier4K       = 6
)

func heightTier(h int) int {
	switch {
	case h >= 2050:
		return tier4K
	case h >= 1400:
		return tier1440p
	case h >= 1040:
		return tier1080p
	case h >= 700:
		return tier720p
	case h >= 460:
		return tier480p
	case h >= 340:
		return tier360p
	default:
		return tierBelow360
	}
}

func widthTier(w int) int {
	switch {
	case w >= 3700:
		return tier4K
	case w >= 2400:
		return tier1440p
	case w >= 1800:
		return tier1080p
	case w >= 1200:
		return tier720p
	case w >= 800:
		return tier480p
	case w >= 600:
		return tier360p
	default:
		return tierBelow360
	}
}

func tierLabel(t int) string {
	switch t {
	case tier4K:
		return "4K"
	case tier1440p:
		return "1440p"
	case tier1080p:
		return "1080p"
	case tier720p:
		return "720p"
	case tier480p:
		return "480p"
	case tier360p:
		return "360p"
	default:
		return ""
	}
}

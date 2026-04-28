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

// Display returns the human-friendly label for the resolution. Two
// tiers of folding apply:
//
//  1. Exact lookup in knownResolutionLabels for canonical and common
//     non-canonical aspect ratios — covers ultrawide cases the height
//     range can't handle on its own (2560x1080 is 1080p, not 1440p).
//  2. Height-range fallback for everything else — folds 4:3 variants
//     (1440x1080 → 1080p, 960x720 → 720p) and near-standard ±5%
//     encodes (1328x720, 1916x1076, …) into the closest tier.
//
// Anything below the 360p height floor falls through to raw WxH —
// SD-PAL / VHS-tier sources are rare enough to deserve the literal
// number rather than a misleading shorthand.
//
// Raw WxH stays available via String() / the JSON serialization for
// callers that need the exact dimensions.
func (r Resolution) Display() string {
	if !r.Known() {
		return ""
	}
	if label, ok := knownResolutionLabels[r.String()]; ok {
		return label
	}
	switch {
	case r.height >= 2050:
		return "4K"
	case r.height >= 1400:
		return "1440p"
	case r.height >= 1040:
		return "1080p"
	case r.height >= 700:
		return "720p"
	case r.height >= 460:
		return "480p"
	case r.height >= 340:
		return "360p"
	default:
		return r.String()
	}
}

// knownResolutionLabels short-circuits the height-range fallback for
// dimensions whose tier is not what their height alone would imply.
// Primarily covers ultrawide and DCI / cinema variants where the
// width is the deciding signal. 4:3 and near-standard cases are left
// to the range fallback since their heights already match the right
// tier.
var knownResolutionLabels = map[string]string{
	// 4K / DCI 4K.
	"3840x2160": "4K",
	"4096x2160": "4K",
	// QHD.
	"2560x1440": "1440p",
	// FHD + ultrawide variants.
	"1920x1080": "1080p",
	"2560x1080": "1080p", // 21:9 ultrawide
	"3440x1440": "1440p", // 21:9 ultrawide QHD
	// HD.
	"1280x720": "720p",
	// SD common variants (NTSC anamorphic, PAL, 16:9 anamorphic).
	"854x480": "480p",
	"720x480": "480p",
	"640x480": "480p",
	"720x576": "480p",
	// Low-res streaming defaults.
	"640x360": "360p",
}

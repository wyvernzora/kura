package media

import "strings"

type Source string

const (
	SourceUnknown Source = "unknown"
	SourceTVRip   Source = "tvrip"
	SourceWebRip  Source = "webrip"
	SourceWebDL   Source = "web-dl"
	// SourceBluRay covers anything sourced from a Blu-ray disc:
	// BDMV/BDISO raw rips, BDRip re-encodes, BD shorthand. Quality
	// bucket is the same — disc-sourced 1080p+ — so kura collapses
	// the distinction.
	SourceBluRay Source = "bluray"
	SourceHDTV   Source = "hdtv"
	SourceDVDRip Source = "dvdrip"
)

func ParseSource(source string) Source {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", "unknown":
		return SourceUnknown
	case "tvrip", "tv":
		return SourceTVRip
	case "webrip":
		return SourceWebRip
	case "web-dl", "webdl":
		return SourceWebDL
	case "bluray", "blu-ray", "bdrip", "bd-rip", "bd", "bdmv", "bdiso":
		return SourceBluRay
	case "hdtv":
		return SourceHDTV
	case "dvd", "dvdrip":
		return SourceDVDRip
	default:
		return Source(strings.ToLower(strings.TrimSpace(source)))
	}
}

func (s Source) String() string {
	if s == "" {
		return string(SourceUnknown)
	}
	return string(s)
}

func (s Source) Display() string {
	switch ParseSource(s.String()) {
	case SourceUnknown:
		return "Unknown"
	case SourceTVRip:
		return "TVRip"
	case SourceWebRip:
		return "WebRip"
	case SourceWebDL:
		return "Web-DL"
	case SourceBluRay:
		return "BluRay"
	case SourceHDTV:
		return "HDTV"
	case SourceDVDRip:
		return "DVDRip"
	default:
		return strings.TrimSpace(s.String())
	}
}

// IsKnown reports whether ParseSource recognizes raw as one of the
// canonical Source constants (vs. a free-form fallthrough). Used by
// filename inference to skip over fields like "1280x720" that ParseSource
// would otherwise pass through as a Source string.
func IsKnown(raw string) bool {
	switch ParseSource(raw) {
	case SourceUnknown,
		SourceTVRip,
		SourceWebRip,
		SourceWebDL,
		SourceBluRay,
		SourceHDTV,
		SourceDVDRip:
		return true
	}
	return false
}

func (s Source) Rank() int {
	switch ParseSource(s.String()) {
	case SourceBluRay:
		return 3
	case SourceWebRip, SourceWebDL:
		return 2
	case SourceTVRip, SourceHDTV, SourceDVDRip:
		return 1
	default:
		return 0
	}
}

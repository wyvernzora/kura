package media

import "strings"

type Source string

const (
	SourceUnknown Source = "unknown"
	SourceTVRip   Source = "tvrip"
	SourceWebRip  Source = "webrip"
	SourceWebDL   Source = "web-dl"
	SourceBDRip   Source = "bdrip"
	SourceBluRay  Source = "bluray"
	SourceHDTV    Source = "hdtv"
	SourceDVDRip  Source = "dvdrip"
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
	case "bdrip", "bd-rip", "bd":
		// "BD" in release filenames is colloquial for BDRip
		// (re-encoded from a Blu-ray); raw disc rips use bdmv / bdiso.
		return SourceBDRip
	case "bluray", "blu-ray", "bdmv", "bdiso":
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
	case SourceBDRip:
		return "BDRip"
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

func (s Source) Rank() int {
	switch ParseSource(s.String()) {
	case SourceBDRip, SourceBluRay:
		return 3
	case SourceWebRip, SourceWebDL:
		return 2
	case SourceTVRip, SourceHDTV, SourceDVDRip:
		return 1
	default:
		return 0
	}
}

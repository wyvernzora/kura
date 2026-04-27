package domain

import (
	"strings"
)

type MediaSource string

const (
	MediaSourceUnknown MediaSource = "unknown"
	MediaSourceTVRip   MediaSource = "tvrip"
	MediaSourceWebRip  MediaSource = "webrip"
	MediaSourceWebDL   MediaSource = "web-dl"
	MediaSourceBDRip   MediaSource = "bdrip"
	MediaSourceBluRay  MediaSource = "bluray"
	MediaSourceHDTV    MediaSource = "hdtv"
	MediaSourceDVDRip  MediaSource = "dvdrip"
)

func ParseMediaSource(source string) MediaSource {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", "unknown":
		return MediaSourceUnknown
	case "tvrip", "tv":
		return MediaSourceTVRip
	case "webrip":
		return MediaSourceWebRip
	case "web-dl", "webdl":
		return MediaSourceWebDL
	case "bdrip":
		return MediaSourceBDRip
	case "bluray", "blu-ray":
		return MediaSourceBluRay
	case "hdtv":
		return MediaSourceHDTV
	case "dvd", "dvdrip":
		return MediaSourceDVDRip
	default:
		return MediaSource(strings.ToLower(strings.TrimSpace(source)))
	}
}

func (s MediaSource) String() string {
	if s == "" {
		return string(MediaSourceUnknown)
	}
	return string(s)
}

func (s MediaSource) Display() string {
	switch ParseMediaSource(s.String()) {
	case MediaSourceUnknown:
		return "Unknown"
	case MediaSourceTVRip:
		return "TVRip"
	case MediaSourceWebRip:
		return "WebRip"
	case MediaSourceWebDL:
		return "Web-DL"
	case MediaSourceBDRip:
		return "BDRip"
	case MediaSourceBluRay:
		return "BluRay"
	case MediaSourceHDTV:
		return "HDTV"
	case MediaSourceDVDRip:
		return "DVDRip"
	default:
		return strings.TrimSpace(s.String())
	}
}

func (s MediaSource) Rank() int {
	switch ParseMediaSource(s.String()) {
	case MediaSourceBDRip, MediaSourceBluRay:
		return 3
	case MediaSourceWebRip, MediaSourceWebDL:
		return 2
	case MediaSourceTVRip, MediaSourceHDTV, MediaSourceDVDRip:
		return 1
	default:
		return 0
	}
}

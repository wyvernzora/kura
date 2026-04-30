package series

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/wyvernzora/kura/internal/refs"
	"golang.org/x/text/unicode/norm"
)

type Inspector interface {
	Inspect(context.Context, string) (MediaInfo, error)
}

type MediaInfo struct {
	VideoCodec   string `json:"videoCodec,omitempty"`
	Resolution   string `json:"resolution,omitempty"`
	AudioCodec   string `json:"audioCodec,omitempty"`
	HasSubtitles bool   `json:"hasSubtitles"`
}

func (m MediaInfo) MarshalJSON() ([]byte, error) {
	type mediaInfo MediaInfo
	out := mediaInfo(m)
	return json.Marshal(out)
}

type Codec string

func ParseCodec(codec string) Codec {
	return Codec(strings.TrimSpace(codec))
}

func (c Codec) String() string {
	return string(c)
}

func (c Codec) Known() bool {
	return strings.TrimSpace(string(c)) != ""
}

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

func (r Resolution) Display() string {
	if !r.Known() {
		return ""
	}
	switch r.String() {
	case "3840x2160", "4096x2160":
		return "4K"
	case "2560x1440":
		return "1440p"
	case "1920x1080":
		return "1080p"
	case "1280x720":
		return "720p"
	case "640x480", "720x480", "854x480":
		return "480p"
	default:
		return r.String()
	}
}

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

type FileTitle string

func ParseFileTitle(title string) (FileTitle, error) {
	clean := CleanFileTitle(title)
	if clean.IsZero() {
		return "", errors.New("library: filesystem title is required")
	}
	if invalidFileTitle(string(clean)) {
		return "", fmt.Errorf("library: invalid filesystem title %q", title)
	}
	return clean, nil
}

func CleanFileTitle(title string) FileTitle {
	return FileTitle(norm.NFC.String(strings.TrimSpace(title)))
}

func (t FileTitle) String() string {
	return string(t)
}

func (t FileTitle) IsZero() bool {
	return strings.TrimSpace(string(t)) == ""
}

func (t FileTitle) EqualName(name string) bool {
	return t == CleanFileTitle(name)
}

func invalidFileTitle(title string) bool {
	return strings.ContainsFunc(title, func(r rune) bool {
		return r == '/' || r == '\\' || r == 0 || unicode.IsControl(r)
	})
}

type MediaFilename string

type MediaFilenameFacts struct {
	Source     MediaSource
	Resolution Resolution
}

func BuildMediaFilename(title FileTitle, episode refs.Episode, facts MediaFilenameFacts, extension string) MediaFilename {
	source := facts.Source.Display()
	resolution := facts.Resolution.Display()
	if resolution == "" {
		resolution = "UnknownResolution"
	}
	return MediaFilename(fmt.Sprintf(
		"%s - %s (%s %s)%s",
		title,
		episode.Marker(),
		source,
		resolution,
		extension,
	))
}

func (f MediaFilename) String() string {
	return string(f)
}

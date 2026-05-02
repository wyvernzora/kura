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

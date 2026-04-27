// Package mediainfo inspects media files with the mediainfo CLI.
package mediainfo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	mediafacts "github.com/wyvernzora/kura/internal/domain"
)

const (
	defaultCommand = "mediainfo"
	defaultTimeout = 30 * time.Second
)

// Inspector shells out to mediainfo and adapts its JSON output into Kura's
// narrow persistent media facts.
type Inspector struct {
	Command string
	Timeout time.Duration
}

// New returns an Inspector using the mediainfo executable on PATH.
func New() Inspector {
	return Inspector{
		Command: defaultCommand,
		Timeout: defaultTimeout,
	}
}

// Inspect runs mediainfo for path and returns the facts Kura persists.
func (i Inspector) Inspect(ctx context.Context, path string) (mediafacts.MediaInfo, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return mediafacts.MediaInfo{}, errors.New("mediainfo: path is required")
	}

	command := i.Command
	if command == "" {
		command = defaultCommand
	}
	timeout := i.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, command, "--Output=JSON", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if runCtx.Err() != nil {
		return mediafacts.MediaInfo{}, fmt.Errorf("mediainfo: inspect %q timed out: %w", path, runCtx.Err())
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return mediafacts.MediaInfo{}, fmt.Errorf("mediainfo: inspect %q: %w: %s", path, err, msg)
		}
		return mediafacts.MediaInfo{}, fmt.Errorf("mediainfo: inspect %q: %w", path, err)
	}

	info, err := ParseJSON(output)
	if err != nil {
		return mediafacts.MediaInfo{}, fmt.Errorf("mediainfo: inspect %q: %w", path, err)
	}
	return info, nil
}

// ParseJSON adapts mediainfo --Output=JSON output into Kura's MediaInfo model.
func ParseJSON(data []byte) (mediafacts.MediaInfo, error) {
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return mediafacts.MediaInfo{}, fmt.Errorf("decode JSON: %w", err)
	}

	out := mediafacts.MediaInfo{}
	for _, track := range doc.Media.Tracks {
		switch strings.ToLower(track.Type) {
		case "video":
			if out.VideoCodec == "" {
				out.VideoCodec = normalizeCodec(firstString(track.Format, track.CodecID))
			}
			if out.Resolution == "" {
				out.Resolution = resolution(track.Width, track.Height)
			}
		case "audio":
			if out.AudioCodec == "" {
				out.AudioCodec = normalizeCodec(firstString(track.Format, track.CodecID))
			}
		case "text":
			out.HasSubtitles = true
		}
	}
	return out, nil
}

type document struct {
	Media media `json:"media"`
}

type media struct {
	Tracks []track `json:"track"`
}

type track struct {
	Type    string `json:"@type"`
	Format  value  `json:"Format"`
	CodecID value  `json:"CodecID"`
	Width   value  `json:"Width"`
	Height  value  `json:"Height"`
}

type value string

func (v *value) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*v = ""
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*v = value(s)
		return nil
	}

	var n json.Number
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&n); err == nil {
		*v = value(n.String())
		return nil
	}

	return nil
}

func firstString(values ...value) string {
	return strings.TrimSpace(string(firstValue(values...)))
}

func firstValue(values ...value) value {
	for _, value := range values {
		clean := strings.TrimSpace(string(value))
		if clean != "" {
			return value
		}
	}
	return ""
}

func normalizeCodec(codec string) string {
	codec = strings.TrimSpace(codec)
	switch strings.ToLower(codec) {
	case "avc", "h264", "h.264", "mpeg-4 avc":
		return "H.264"
	case "hevc", "h265", "h.265":
		return "HEVC"
	default:
		return codec
	}
}

func resolution(widthValue value, heightValue value) string {
	width := integer(widthValue)
	height := integer(heightValue)
	if width == 0 || height == 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", width, height)
}

func integer(value value) int {
	text := strings.TrimSpace(string(value))
	if text == "" {
		return 0
	}
	text = strings.ReplaceAll(text, " ", "")
	text = strings.ReplaceAll(text, ",", "")
	if parsed, err := strconv.Atoi(text); err == nil {
		return parsed
	}
	if parsed, err := strconv.ParseFloat(text, 64); err == nil {
		return int(parsed)
	}
	return 0
}

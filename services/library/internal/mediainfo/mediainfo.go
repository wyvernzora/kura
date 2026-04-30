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

	"github.com/wyvernzora/kura/internal/domain/media"
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

// MissingBinaryError signals that the configured mediainfo executable
// could not be found on PATH. The Error message includes a remediation
// hint so operators know which package to install.
type MissingBinaryError struct {
	Command string
	Inner   error
}

func (e *MissingBinaryError) Error() string {
	return fmt.Sprintf(
		"mediainfo: %q not found on PATH. Install it (macOS: `brew install mediainfo`; Debian/Ubuntu: `apt install mediainfo`; Alpine: `apk add mediainfo`) or set the Inspector.Command to the full path.",
		e.Command,
	)
}

func (e *MissingBinaryError) Unwrap() error { return e.Inner }

// New returns an Inspector using the mediainfo executable on PATH.
func New() Inspector {
	return Inspector{
		Command: defaultCommand,
		Timeout: defaultTimeout,
	}
}

// Inspect runs mediainfo for path and returns the facts Kura persists.
func (i Inspector) Inspect(ctx context.Context, path string) (media.Info, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return media.Info{}, errors.New("mediainfo: path is required")
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
		return media.Info{}, fmt.Errorf("mediainfo: inspect %q timed out: %w", path, runCtx.Err())
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return media.Info{}, &MissingBinaryError{Command: command, Inner: err}
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return media.Info{}, fmt.Errorf("mediainfo: inspect %q: %w: %s", path, err, msg)
		}
		return media.Info{}, fmt.Errorf("mediainfo: inspect %q: %w", path, err)
	}

	info, err := ParseJSON(output)
	if err != nil {
		return media.Info{}, fmt.Errorf("mediainfo: inspect %q: %w", path, err)
	}
	return info, nil
}

// ParseJSON adapts mediainfo --Output=JSON output into Kura's media.Info model.
func ParseJSON(data []byte) (media.Info, error) {
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return media.Info{}, fmt.Errorf("decode JSON: %w", err)
	}

	out := media.Info{}
	for _, track := range doc.Media.Tracks {
		switch strings.ToLower(track.Type) {
		case "general":
			if out.Title == "" {
				out.Title = firstString(track.Title, track.Movie)
			}
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
	Media mediaPayload `json:"media"`
}

type mediaPayload struct {
	Tracks []track `json:"track"`
}

type track struct {
	Type    string `json:"@type"`
	Format  value  `json:"Format"`
	CodecID value  `json:"CodecID"`
	Width   value  `json:"Width"`
	Height  value  `json:"Height"`
	// Title / Movie are alternate keys mediainfo emits for the
	// container's General-track title. mkvmerge writes Title; ffmpeg
	// commonly writes Movie. firstString picks the first non-empty.
	Title value `json:"Title"`
	Movie value `json:"Movie"`
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

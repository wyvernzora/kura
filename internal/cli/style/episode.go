package style

import (
	"strings"

	"github.com/ttacon/chalk"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/response"
)

// EpisodeStatus colors a response.Status string. Used by show, scan, and
// reconcile renders.
func EpisodeStatus(status string, styled bool) string {
	value := strings.TrimSpace(status)
	if !styled {
		return value
	}
	switch value {
	case string(response.StatusMissing):
		return Orange(value)
	case string(response.StatusUnavailable):
		return chalk.Bold.TextStyle(chalk.Red.Color(value))
	case string(response.StatusPresent):
		return chalk.Green.Color(value)
	case string(response.StatusPending):
		return chalk.Dim.TextStyle(Gray(value))
	case string(response.StatusStaged), string(response.StatusStagedReplacement):
		return chalk.Yellow.Color(value)
	default:
		return value
	}
}

// MediaSource colors a source label after running it through media.ParseSource.Display.
func MediaSource(source string, styled bool) string {
	value := media.ParseSource(source).Display()
	if !styled {
		return value
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bdrip", "bluray", "blu-ray":
		return chalk.Green.Color(value)
	case "web-dl", "webdl", "web-rip", "webrip":
		return chalk.Yellow.Color(value)
	case "tv", "hdtv", "tvrip", "tv-rip":
		return Orange(value)
	case "unknown":
		return chalk.Red.Color(value)
	default:
		return value
	}
}

// MediaResolution colors a resolution label, normalizing through
// media.ParseResolution.Display when possible.
func MediaResolution(resolution string, styled bool) string {
	value := strings.TrimSpace(resolution)
	if parsed, err := media.ParseResolution(value); err == nil && parsed.Known() {
		value = parsed.Display()
	}
	if !styled {
		return value
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "4k":
		return chalk.Blue.Color(value)
	case "1080p":
		return chalk.Green.Color(value)
	case "720p":
		return chalk.Red.Color(value)
	case "":
		return value
	default:
		return Orange(value)
	}
}

// Retired styles a cell as struck-through and dimmed, used for the
// "this active record will be replaced" row when both active and staged
// records are present.
func Retired(value string) string {
	if value == "" {
		return ""
	}
	return chalk.Dim.TextStyle(chalk.Strikethrough.TextStyle(value))
}

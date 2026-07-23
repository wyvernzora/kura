package style

import (
	"strings"

	"github.com/wyvernzora/kura/services/library/internal/domain/media"
	"github.com/wyvernzora/kura/services/library/internal/response"
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
	case string(response.StatusPresent):
		return Green(value)
	case string(response.StatusPending):
		return Dim(Gray(value))
	case string(response.StatusStaged), string(response.StatusStagedReplacement):
		return Yellow(value)
	default:
		return value
	}
}

// MediaSource colors a source label after running it through media.ParseSource.Display.
// Empty input passes through as empty so callers can suppress the column for
// rows where there is no source to report (e.g. Missing or Pending episodes).
func MediaSource(source string, styled bool) string {
	if strings.TrimSpace(source) == "" {
		return ""
	}
	value := media.ParseSource(source).Display()
	if !styled {
		return value
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bdrip", "bluray", "blu-ray":
		return Green(value)
	case "web-dl", "webdl", "web-rip", "webrip":
		return Yellow(value)
	case "tv", "hdtv", "tvrip", "tv-rip":
		return Orange(value)
	case "unknown":
		// Dim/gray, not red — Unknown is a "we don't have a source
		// tag" state, not an error to fix.
		return Dim(Gray(value))
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
		return Blue(value)
	case "1080p":
		return Green(value)
	case "720p":
		return Red(value)
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
	return Dim(Strikethrough(value))
}

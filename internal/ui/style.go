package ui

import (
	"strings"

	"github.com/ttacon/chalk"
	"github.com/wyvernzora/kura/internal/library"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/series"
)

func renderStatus(status string, style bool) string {
	value := strings.TrimSpace(status)
	if !style {
		return value
	}
	switch value {
	case string(series.EpisodeStatusMissing):
		return orange(value)
	case string(series.EpisodeStatusUnavailable):
		return chalk.Bold.TextStyle(chalk.Red.Color(value))
	case string(series.EpisodeStatusPresent):
		return chalk.Green.Color(value)
	case string(series.EpisodeStatusPending):
		return chalk.Dim.TextStyle(gray(value))
	case string(series.EpisodeStatusStaged), string(series.EpisodeStatusStagedReplacement):
		return chalk.Yellow.Color(value)
	case string(series.ScanStatusAdded):
		return chalk.Green.Color(value)
	case string(series.ScanStatusUnchanged):
		return chalk.Dim.TextStyle(gray(value))
	case string(series.ScanStatusUpdated), string(series.ScanStatusReplaced):
		return chalk.Yellow.Color(value)
	case string(series.ScanStatusRemoved):
		return chalk.Bold.TextStyle(chalk.Red.Color(value))
	default:
		return value
	}
}

func renderListStatus(status string, style bool) string {
	value := strings.TrimSpace(status)
	if !style {
		return value
	}
	base := strings.TrimSuffix(value, "*")
	suffix := strings.TrimPrefix(value, base)
	switch base {
	case string(library.ListStatusUntracked):
		return chalk.Dim.TextStyle(gray(base)) + suffix
	case string(library.ListStatusComplete):
		return chalk.Green.Color(base) + suffix
	case string(library.ListStatusIncomplete):
		return chalk.Bold.TextStyle(chalk.Red.Color(base)) + suffix
	case string(library.ListStatusAiring):
		return chalk.Blue.Color(base) + suffix
	case string(library.ListStatusError):
		return chalk.Bold.TextStyle(chalk.Red.Color(base)) + suffix
	default:
		return value
	}
}

func renderMediaSource(source string, style bool) string {
	value := media.ParseSource(source).Display()
	if !style {
		return value
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bdrip", "bluray", "blu-ray":
		return chalk.Green.Color(value)
	case "web-dl", "webdl", "web-rip", "webrip":
		return chalk.Yellow.Color(value)
	case "tv", "hdtv", "tvrip", "tv-rip":
		return orange(value)
	case "unknown":
		return chalk.Red.Color(value)
	default:
		return value
	}
}

func renderMediaResolution(resolution string, style bool) string {
	value := strings.TrimSpace(resolution)
	if parsed, err := media.ParseResolution(value); err == nil && parsed.Known() {
		value = parsed.Display()
	}
	if !style {
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
		return orange(value)
	}
}

func orange(value string) string {
	return "\x1b[38;5;208m" + value + "\x1b[39m"
}

func gray(value string) string {
	return "\x1b[90m" + value + "\x1b[39m"
}

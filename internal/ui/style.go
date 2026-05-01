package ui

import (
	"strings"

	"github.com/ttacon/chalk"
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
	case string(series.ScanStatusNew):
		return chalk.Green.Color(value)
	case string(series.ScanStatusExisting):
		return chalk.Dim.TextStyle(gray(value))
	case string(series.ScanStatusUpdated), string(series.ScanStatusReplaced):
		return chalk.Yellow.Color(value)
	default:
		return value
	}
}

func renderMediaSource(source string, style bool) string {
	value := series.ParseMediaSource(source).Display()
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
	if parsed, err := series.ParseResolution(value); err == nil && parsed.Known() {
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

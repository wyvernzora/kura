package layout

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/wyvernzora/kura/internal/series/state"
)

type FilesystemIssue struct {
	Record string `json:"record"`
	Path   string `json:"path"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

func EpisodeFilesystemIssues(seriesDir SeriesDir, episode state.Episode) []FilesystemIssue {
	var issues []FilesystemIssue
	if episode.Active != nil {
		issues = append(issues, RecordFilesystemIssues(seriesDir, "active", *episode.Active, false)...)
	}
	if episode.Staged != nil {
		issues = append(issues, RecordFilesystemIssues(seriesDir, "staged", *episode.Staged, true)...)
	}
	return issues
}

func RecordFilesystemIssues(seriesDir SeriesDir, recordName string, media state.MediaRecord, absolute bool) []FilesystemIssue {
	var issues []FilesystemIssue
	issues = append(issues, PathFilesystemIssues(seriesDir, recordName, "media", media.Path, absolute)...)
	for _, companion := range media.Companions {
		issues = append(issues, PathFilesystemIssues(seriesDir, recordName, "companion", companion.Path, absolute)...)
	}
	return issues
}

func PathFilesystemIssues(seriesDir SeriesDir, recordName string, kind string, rawPath string, absolute bool) []FilesystemIssue {
	path := rawPath
	if !absolute {
		joined, err := seriesDir.JoinRel(rawPath)
		if err != nil {
			return []FilesystemIssue{{
				Record: recordName,
				Path:   rawPath,
				Code:   recordName + "_" + kind + "_invalid_path",
				Reason: err.Error(),
			}}
		}
		path = joined
	} else if !filepath.IsAbs(path) {
		return []FilesystemIssue{{
			Record: recordName,
			Path:   rawPath,
			Code:   recordName + "_" + kind + "_invalid_path",
			Reason: "path must be absolute",
		}}
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return []FilesystemIssue{{
			Record: recordName,
			Path:   rawPath,
			Code:   recordName + "_" + kind + "_missing",
			Reason: "path does not exist",
		}}
	}
	if err != nil {
		return []FilesystemIssue{{
			Record: recordName,
			Path:   rawPath,
			Code:   recordName + "_" + kind + "_stat_error",
			Reason: err.Error(),
		}}
	}
	if info.IsDir() {
		return []FilesystemIssue{{
			Record: recordName,
			Path:   rawPath,
			Code:   recordName + "_" + kind + "_not_file",
			Reason: "path is a directory",
		}}
	}
	return nil
}

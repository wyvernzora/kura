package series

import (
	"errors"
	"os"
	"path/filepath"
)

func episodeFilesystemIssues(seriesDir SeriesDir, episode episodeState) []FilesystemIssue {
	var issues []FilesystemIssue
	if episode.Active != nil {
		issues = append(issues, recordFilesystemIssues(seriesDir, "active", *episode.Active, false)...)
	}
	if episode.Staged != nil {
		issues = append(issues, recordFilesystemIssues(seriesDir, "staged", *episode.Staged, true)...)
	}
	return issues
}

func recordFilesystemIssues(seriesDir SeriesDir, recordName string, media MediaRecord, absolute bool) []FilesystemIssue {
	var issues []FilesystemIssue
	issues = append(issues, pathFilesystemIssue(seriesDir, recordName, "media", media.Path, absolute)...)
	for _, companion := range media.Companions {
		issues = append(issues, pathFilesystemIssue(seriesDir, recordName, "companion", companion.Path, absolute)...)
	}
	return issues
}

func pathFilesystemIssue(seriesDir SeriesDir, recordName string, kind string, rawPath string, absolute bool) []FilesystemIssue {
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

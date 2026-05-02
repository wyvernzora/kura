package layout

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/series"
)

type FilesystemIssue struct {
	Record string `json:"record"`
	Path   string `json:"path"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

func EpisodeFilesystemIssues(seriesDir SeriesDir, episode series.Episode) []FilesystemIssue {
	var issues []FilesystemIssue
	if episode.Active != nil {
		issues = append(issues, RecordFilesystemIssues(seriesDir, "active", *episode.Active)...)
	}
	if episode.Staged != nil {
		issues = append(issues, RecordFilesystemIssues(seriesDir, "staged", *episode.Staged)...)
	}
	return issues
}

func RecordFilesystemIssues(seriesDir SeriesDir, recordName string, record media.Record) []FilesystemIssue {
	var issues []FilesystemIssue
	issues = append(issues, PathFilesystemIssues(seriesDir, recordName, "media", record.Path)...)
	for _, companion := range record.Companions {
		issues = append(issues, PathFilesystemIssues(seriesDir, recordName, "companion", companion.Path)...)
	}
	return issues
}

// PathFilesystemIssues stats path and reports filesystem issues. Path must be
// absolute; series.Series records carry absolute paths in memory after
// seriesfile.Load.
func PathFilesystemIssues(seriesDir SeriesDir, recordName string, kind string, rawPath string) []FilesystemIssue {
	path := rawPath
	if !filepath.IsAbs(path) {
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

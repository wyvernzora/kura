package workflow

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/wyvernzora/kura/internal/domain/media"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/storage/seriesdir"
)

// filesystemIssue describes one inconsistency between a recorded media
// path and what the filesystem actually has at that path. Used by Show
// to surface drift without persisting it.
type filesystemIssue struct {
	Record string
	Path   string
	Code   string
	Reason string
}

// episodeFilesystemIssues stat-checks the active and staged records of an
// episode and returns the union of detected issues.
func episodeFilesystemIssues(_ seriesdir.SeriesDir, episode domainseries.Episode) []filesystemIssue {
	var issues []filesystemIssue
	if episode.Active != nil {
		issues = append(issues, recordFilesystemIssues("active", *episode.Active)...)
	}
	if episode.Staged != nil {
		issues = append(issues, recordFilesystemIssues("staged", *episode.Staged)...)
	}
	return issues
}

func recordFilesystemIssues(recordName string, record media.Record) []filesystemIssue {
	var issues []filesystemIssue
	issues = append(issues, pathFilesystemIssues(recordName, "media", record.Path)...)
	for _, companion := range record.Companions {
		issues = append(issues, pathFilesystemIssues(recordName, "companion", companion.Path)...)
	}
	return issues
}

// pathFilesystemIssues stats path and reports inconsistencies. Path must
// be absolute; series records carry absolute paths in memory after
// seriesfile.Load.
func pathFilesystemIssues(recordName, kind, rawPath string) []filesystemIssue {
	if !filepath.IsAbs(rawPath) {
		return []filesystemIssue{{
			Record: recordName,
			Path:   rawPath,
			Code:   recordName + "_" + kind + "_invalid_path",
			Reason: "path must be absolute",
		}}
	}
	info, err := os.Stat(rawPath)
	if errors.Is(err, os.ErrNotExist) {
		return []filesystemIssue{{
			Record: recordName,
			Path:   rawPath,
			Code:   recordName + "_" + kind + "_missing",
			Reason: "path does not exist",
		}}
	}
	if err != nil {
		return []filesystemIssue{{
			Record: recordName,
			Path:   rawPath,
			Code:   recordName + "_" + kind + "_stat_error",
			Reason: err.Error(),
		}}
	}
	if info.IsDir() {
		return []filesystemIssue{{
			Record: recordName,
			Path:   rawPath,
			Code:   recordName + "_" + kind + "_not_file",
			Reason: "path is a directory",
		}}
	}
	return nil
}

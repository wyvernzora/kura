package series

import "github.com/wyvernzora/kura/internal/series/layout"

func episodeFilesystemIssues(seriesDir SeriesDir, episode episodeState) []FilesystemIssue {
	return layout.EpisodeFilesystemIssues(seriesDir, episode)
}

func pathFilesystemIssue(seriesDir SeriesDir, recordName string, kind string, rawPath string) []FilesystemIssue {
	return layout.PathFilesystemIssues(seriesDir, recordName, kind, rawPath)
}

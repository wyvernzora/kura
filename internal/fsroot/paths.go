package fsroot

import (
	"path/filepath"
)

const (
	KuraDir        = ".kura"
	KuraTrashDir   = "trash"
	IndexFileName  = "index.tsv"
	SeriesFileName = "series.json"
)

func IndexMetadataPath(libraryRoot string) string {
	return filepath.Join(libraryRoot, KuraDir, IndexFileName)
}

func SeriesMetadataPath(seriesDir string) string {
	return filepath.Join(seriesDir, KuraDir, SeriesFileName)
}

package fsroot

import (
	"path/filepath"
)

const (
	KuraDir        = ".kura"
	KuraTrashDir   = "trash"
	SeriesFileName = "series.json"
	StagedFileName = "staged.json"
	TrashFileName  = "trash.json"
)

func SeriesMetadataPath(seriesDir string) string {
	return filepath.Join(seriesDir, KuraDir, SeriesFileName)
}

func TrashMetadataPath(seriesDir string) string {
	return filepath.Join(seriesDir, KuraDir, TrashFileName)
}

func StagedMetadataPath(seriesDir string) string {
	return filepath.Join(seriesDir, KuraDir, StagedFileName)
}
